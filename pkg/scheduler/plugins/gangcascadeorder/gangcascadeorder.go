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
// treats as equal, it offers the gang that spans the fewest nodes first (with total gang
// size as a secondary tiebreak). It only ever breaks ties — it never reorders victims of
// different priority — so it lowers reclaim collateral without weakening any preemption or
// fairness guarantee. Unlike gpujoborder's GPU-size tiebreak, node span distinguishes a
// large self-contained gang (cheap to evict for whole-node reclaim) from a small gang that
// cascades across several nodes.
package gangcascadeorder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type gangCascadeOrderPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &gangCascadeOrderPlugin{}
}

func (p *gangCascadeOrderPlugin) Name() string {
	return "gangcascadeorder"
}

func (p *gangCascadeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddVictimOrderFn(p.VictimOrderFn)
}

// VictimOrderFn returns <0 when l should be evicted before r. It engages only for
// equal-priority victims (returning 0 otherwise so higher-priority protections are
// preserved), preferring the smaller cross-node footprint: fewer spanned nodes first,
// then fewer total gang pods.
func (p *gangCascadeOrderPlugin) VictimOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lSpan, lPods := gangFootprint(lv)
	rSpan, rPods := gangFootprint(rv)

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

// gangFootprint reports how many distinct nodes the gang's allocated pods occupy (its
// cross-node cascade span) and the number of allocated pods.
func gangFootprint(pg *podgroup_info.PodGroupInfo) (span int, pods int) {
	nodes := map[string]struct{}{}
	allocated := pg.GetAllAllocatedPods()
	for _, task := range allocated {
		if task.NodeName == "" {
			continue
		}
		nodes[task.NodeName] = struct{}{}
	}
	return len(nodes), len(allocated)
}
