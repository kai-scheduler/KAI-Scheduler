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

// OnSessionOpen registers this plugin's comparator via AddVictimOrderFn,
// NOT AddJobOrderFn. This plugin is scoped to victim/eviction selection
// only, per the real, confirmed distinction between the ordering and
// victim-selection paths (see VictimOrderFn in session_plugins.go) --
// pending-job allocation ordering is intentionally left untouched.
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
