// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpujoborder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	ModeEvictLargerFirst  = "evict-larger-first"
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

func (rp *gpuJobOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddVictimOrderFn(rp.VictimOrderFn)
}

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
	default:
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
