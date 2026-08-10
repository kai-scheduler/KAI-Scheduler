// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package gpujoborder implements a GPU-count-based victim-selection
// tiebreak, used only when two jobs of equal priority are otherwise tied
// (including by any other registered JobOrderFn, such as elastic's
// at-min/above-min protection) and the scheduler must choose which one
// to evict. It has no effect on pending-job allocation ordering, and no
// effect whenever an existing JobOrderFn-registered plugin already has an
// opinion on the comparison.
package gpujoborder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	// ModeEvictLargerFirst prefers evicting the job requesting MORE GPUs
	// when a tie must be broken. Named for the eviction outcome directly
	// (not "prefer-larger", which reads ambiguously as either "prefer to
	// evict the larger job" or "prefer to keep/favor the larger job") --
	// per review discussion, this is deliberately unambiguous since the
	// config string is hard to rename once real users depend on it.
	ModeEvictLargerFirst = "evict-larger-first"
	// ModeEvictSmallerFirst prefers evicting the job requesting FEWER
	// GPUs when a tie must be broken.
	ModeEvictSmallerFirst = "evict-smaller-first"
)

type gpuJobOrderPlugin struct {
	mode string
}

func New(arguments framework.PluginArguments) framework.Plugin {
	mode := arguments.GetString("mode", ModeEvictLargerFirst)
	if mode != ModeEvictLargerFirst && mode != ModeEvictSmallerFirst {
		log.InfraLogger.Warningf("gpujoborder: unrecognized mode %q, defaulting to %s", mode, ModeEvictLargerFirst)
		mode = ModeEvictLargerFirst
	}
	return &gpuJobOrderPlugin{mode: mode}
}

func (rp *gpuJobOrderPlugin) Name() string {
	return "gpujoborder"
}

// OnSessionOpen registers this plugin's comparator via AddVictimOrderFn,
// NOT AddJobOrderFn. This plugin is scoped to victim/eviction selection
// only, and only applies as a tiebreak after every registered JobOrderFn
// (priority.go, elastic.go, etc.) has already had a chance to decide --
// see Session.VictimOrderFn for the real composition order.
func (rp *gpuJobOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddVictimOrderFn(rp.VictimOrderFn)
}

// VictimOrderFn returns -1 when l is the BETTER victim (should be evicted
// first), matching VictimOrderFn's direct (non-inverted) contract -- no
// external sign flip is applied or needed here.
func (rp *gpuJobOrderPlugin) VictimOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lGPU := lv.GetAliveTasksRequestedGPUs()
	rGPU := rv.GetAliveTasksRequestedGPUs()

	switch rp.mode {
	case ModeEvictSmallerFirst:
		if lGPU < rGPU {
			return -1
		}
		if lGPU > rGPU {
			return 1
		}
	default: // ModeEvictLargerFirst
		if lGPU > rGPU {
			return -1
		}
		if lGPU < rGPU {
			return 1
		}
	}
	return 0
}

func (rp *gpuJobOrderPlugin) OnSessionClose(_ *framework.Session) {}
