// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpujoborder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	ModePreferLarger  = "prefer-larger"
	ModePreferSmaller = "prefer-smaller"
)

type gpuJobOrderPlugin struct {
	mode string
}

func New(arguments framework.PluginArguments) framework.Plugin {
	mode := arguments.GetString("mode", ModePreferLarger)
	if mode != ModePreferLarger && mode != ModePreferSmaller {
		log.InfraLogger.Warningf("gpujoborder: unrecognized mode %q, defaulting to prefer-larger", mode)
		mode = ModePreferLarger
	}
	return &gpuJobOrderPlugin{mode: mode}
}

func (rp *gpuJobOrderPlugin) Name() string {
	return "gpujoborder"
}

func (rp *gpuJobOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(rp.JobOrderFn)
}

func (rp *gpuJobOrderPlugin) JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lGPU := lv.GetAliveTasksRequestedGPUs()
	rGPU := rv.GetAliveTasksRequestedGPUs()

	switch rp.mode {
	case ModePreferSmaller:
		if lGPU < rGPU {
			return -1
		}
		if lGPU > rGPU {
			return 1
		}
	default: // ModePreferLarger
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
