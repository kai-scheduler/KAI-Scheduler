// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package gangcascadeorder is a victim-order tiebreak that, among equal-priority victims,
// prefers evictions destroying no gang, then a smaller cross-node cascade, then fewer pods.
package gangcascadeorder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type footprint struct {
	span     int
	pods     int
	survives bool
}

// footprintKey memoizes a footprint per session; the active count invalidates it on change.
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

// VictimOrderFn returns <0 if l should be evicted before r. It only breaks ties among
// equal-priority victims: fewest gangs destroyed, then smaller cross-node cascade, then fewer pods.
func (p *gangCascadeOrderPlugin) VictimOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lSpan, lPods, lSurvives := p.gangFootprint(lv)
	rSpan, rPods, rSurvives := p.gangFootprint(rv)

	if lSurvives != rSurvives {
		if lSurvives {
			return -1
		}
		return 1
	}
	if lSpan != rSpan {
		if lSpan < rSpan {
			return -1
		}
		return 1
	}
	if lPods != rPods {
		if lPods < rPods {
			return -1
		}
		return 1
	}
	return 0
}

func (p *gangCascadeOrderPlugin) OnSessionClose(_ *framework.Session) {}

// gangFootprint scores the tasks GetTasksToEvict would actually evict: whether the gang
// survives (elastic surplus shed vs teardown), the nodes touched, and the pods removed.
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
