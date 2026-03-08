// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgrouporder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const (
	lPrioritized        = -1
	rPrioritized        = 1
	equalPrioritization = 0
)

type subGroupOrderPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &subGroupOrderPlugin{}
}

func (sgop *subGroupOrderPlugin) Name() string {
	return "subgrouporder"
}

func (sgop *subGroupOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddPodSetOrderFn(PodSetOrderFn)
}

func PodSetOrderFn(l, r interface{}) int {
	lv := l.(*subgroup_info.PodSet)
	rv := r.(*subgroup_info.PodSet)

	lNumActiveTasks := lv.GetNumActiveAllocatedTasks()
	rNumActiveTasks := rv.GetNumActiveAllocatedTasks()

	// Prioritize SubGroup below minAvailable
	lGangSatisfied := lNumActiveTasks >= int(lv.GetMinAvailable())
	rGangSatisfied := rNumActiveTasks >= int(rv.GetMinAvailable())
	if !lGangSatisfied && !rGangSatisfied {
		return equalPrioritization
	}

	if !lGangSatisfied {
		return lPrioritized
	}
	if !rGangSatisfied {
		return rPrioritized
	}

	// Above minAvailable prioritize SubGroup with lower allocation ratio
	lAllocationRatio := float64(lNumActiveTasks) / float64(lv.GetMinAvailable())
	rAllocationRatio := float64(rNumActiveTasks) / float64(rv.GetMinAvailable())
	if lAllocationRatio < rAllocationRatio {
		return lPrioritized
	}
	if rAllocationRatio < lAllocationRatio {
		return rPrioritized
	}
	return equalPrioritization
}

func (sgop *subGroupOrderPlugin) OnSessionClose(_ *framework.Session) {}
