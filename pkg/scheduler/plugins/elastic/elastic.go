// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package elastic

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type elasticPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &elasticPlugin{}
}

func (pp *elasticPlugin) Name() string {
	return "elastic"
}

func (pp *elasticPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(JobOrderFn)
}

func JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	lvBelowMinAvailable, lvAboveMinAvailable, lvExactlyAtMinAvailable := minAvailableState(lv)
	rvBelowMinAvailable, rvAboveMinAvailable, rvExactlyAtMinAvailable := minAvailableState(rv)

	if lvBelowMinAvailable && !rvBelowMinAvailable {
		return -1
	}

	if lvExactlyAtMinAvailable && rvAboveMinAvailable {
		return -1
	}

	if !lvBelowMinAvailable && rvBelowMinAvailable {
		return 1
	}

	if lvAboveMinAvailable && rvExactlyAtMinAvailable {
		return 1
	}

	// TODO: consider the number of extra pods for elastic jobs?

	return 0
}

func minAvailableState(pgi *podgroup_info.PodGroupInfo) (bool, bool, bool) {
	belowMinAvailable, aboveMinAvailable := subGroupSetState(pgi.RootSubGroupSet)
	if belowMinAvailable {
		return true, false, false
	}
	return false, aboveMinAvailable, !aboveMinAvailable
}

func subGroupSetState(subGroupSet *subgroup_info.SubGroupSet) (bool, bool) {
	if subGroupSet == nil {
		return false, false
	}
	aboveMinAvailable := false
	for _, podSet := range subGroupSet.GetDirectPodSets() {
		below, above := podSetState(podSet)
		if below {
			return true, false
		}
		aboveMinAvailable = aboveMinAvailable || above
	}
	for _, child := range subGroupSet.GetDirectSubgroupsSets() {
		below, above := subGroupSetState(child)
		if below {
			return true, false
		}
		aboveMinAvailable = aboveMinAvailable || above
	}
	return false, aboveMinAvailable
}

func podSetState(podSet *subgroup_info.PodSet) (bool, bool) {
	numAllocatedTasks := int32(podSet.GetNumActiveAllocatedTasks())
	return numAllocatedTasks < podSet.GetMinAvailable(), numAllocatedTasks > podSet.GetMinAvailable()
}

func (pp *elasticPlugin) OnSessionClose(_ *framework.Session) {}
