// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package gangcascadeorder registers a victim-order tiebreaker that prefers evicting
// gangs with a smaller cross-node footprint first.
//
// Reclaim/preempt frees capacity for a pending job by evicting victim gangs. Because a
// gang evicts atomically (dropping any member below minMember tears down the whole gang),
// evicting a victim whose members are spread across many nodes cascades far beyond the
// node the scheduler is actually trying to free — a "wide" gang spanning 8 nodes costs 8
// pods to reclaim a single GPU on the target node, while a self-contained single-node gang
// costs only its local pods. The scenario search commits the first feasible eviction it
// finds (first-solved-wins) in victim-queue order, so the order victims are offered decides
// how much collateral cascade a reclaim incurs.
//
// This plugin keys on that cascade cost: among victims the priority/fairness chain already
// treats as equal, it prefers, in order, (1) evictions that destroy no gang — shedding an
// elastic gang's surplus over tearing a gang down, since a surviving eviction loses no
// workload and its evicted spares are not a cascade at all; (2) the smaller cross-node
// cascade among gang teardowns; and (3) fewer evicted pods. It only ever breaks ties — it
// never reorders victims of different priority — so it lowers reclaim collateral without
// weakening any preemption or fairness guarantee. Unlike gpujoborder's GPU-size tiebreak,
// it distinguishes a large self-contained gang (cheap to evict for whole-node reclaim) from
// a small gang that cascades across several nodes, and a spare-shed from a teardown.
package gangcascadeorder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// footprint is the cross-node cascade cost of reclaiming from a gang.
type footprint struct {
	span     int
	pods     int
	survives bool
}

// footprintKey memoizes a gang's footprint for the duration of a session. It includes
// the active-allocated count so the entry is invalidated whenever the gang gains or loses
// allocated tasks (which is exactly what changes the eviction set).
type footprintKey struct {
	uid    common_info.PodGroupID
	active int
}

type gangCascadeOrderPlugin struct {
	ssn   *framework.Session
	cache map[footprintKey]footprint
}

func New(_ framework.PluginArguments) framework.Plugin {
	return &gangCascadeOrderPlugin{cache: map[footprintKey]footprint{}}
}

func (p *gangCascadeOrderPlugin) Name() string {
	return "gangcascadeorder"
}

func (p *gangCascadeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	p.ssn = ssn
	p.cache = map[footprintKey]footprint{}
	ssn.AddVictimOrderFn(p.VictimOrderFn)
}

// VictimOrderFn returns <0 when l should be evicted before r. It engages only for
// equal-priority victims (returning 0 otherwise so higher-priority protections are
// preserved). Among equal-priority victims it prefers, in order: evictions that destroy
// no gang (shedding elastic surplus over tearing a gang down), then the smaller cross-node
// cascade, then fewer evicted pods.
func (p *gangCascadeOrderPlugin) VictimOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lSpan, lPods, lSurvives := p.gangFootprint(lv)
	rSpan, rPods, rSurvives := p.gangFootprint(rv)

	// 1. Fewer gangs killed first. A surviving (spare-shed) eviction destroys no
	//    workload; a teardown destroys one. Never kill a gang when a spare shed frees
	//    the same capacity. Node span is only a real cascade when a gang actually dies.
	if lSurvives != rSurvives {
		if lSurvives {
			return -1
		}
		return 1
	}
	// 2. Smaller cross-node cascade.
	if lSpan != rSpan {
		if lSpan < rSpan {
			return -1
		}
		return 1
	}
	// 3. Fewer evicted pods.
	if lPods != rPods {
		if lPods < rPods {
			return -1
		}
		return 1
	}
	return 0
}

func (p *gangCascadeOrderPlugin) OnSessionClose(_ *framework.Session) {}

// gangFootprint reports the cost of reclaiming from this gang: whether the gang survives
// the eviction, how many distinct nodes the eviction touches (its cross-node cascade
// span), and how many pods it removes.
//
// It scores the tasks that would *actually* be evicted, not the whole gang. For an
// elastic gang running above minMember, GetTasksToEvict returns only the surplus tasks
// that can be shed while the gang stays gang-satisfied (and reports that the gang still
// has active tasks afterwards), so a wide elastic gang with one surplus pod is correctly
// scored as a surviving, one-pod, one-node eviction rather than a full teardown. For a
// gang at minMember it returns the whole gang and no survivors, since dropping any member
// cascades every pod across every node the gang occupies.
func (p *gangCascadeOrderPlugin) gangFootprint(pg *podgroup_info.PodGroupInfo) (span int, pods int, survives bool) {
	key := footprintKey{uid: pg.UID, active: pg.GetActiveAllocatedTasksCount()}
	if p.cache != nil {
		if f, ok := p.cache[key]; ok {
			return f.span, f.pods, f.survives
		}
	}

	var evicted []*pod_info.PodInfo
	if p.ssn != nil {
		evicted, survives = podgroup_info.GetTasksToEvict(pg, p.ssn.SubGroupOrderFn, p.ssn.TaskOrderFn)
	}
	if len(evicted) == 0 {
		// No surplus to shed: reclaiming from this gang tears down the whole gang.
		evicted = pg.GetAllAllocatedPods()
		survives = false
	}
	nodes := map[string]struct{}{}
	for _, task := range evicted {
		if task.NodeName == "" {
			continue
		}
		nodes[task.NodeName] = struct{}{}
	}
	span, pods = len(nodes), len(evicted)

	if p.cache != nil {
		p.cache[key] = footprint{span: span, pods: pods, survives: survives}
	}
	return span, pods, survives
}
