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
	ssn.AddSubGroupOrderFn(SubGroupOrderFn)
}

// SubGroupOrderFn orders two subgroups by their allocation ratio (allocated/threshold).
// For subGroupSets, it is the allocation ratio of the direct children. For podSets, it is the allocation ratio of the tasks.
func SubGroupOrderFn(l, r interface{}) int {
	lv := l.(subgroup_info.SubGroupMember)
	rv := r.(subgroup_info.SubGroupMember)
	return orderByAllocationRatio(
		lv.GetNumActiveAllocatedMembers(), lv.GetMinMembersToSatisfy(),
		rv.GetNumActiveAllocatedMembers(), rv.GetMinMembersToSatisfy(),
	)
}

// orderByAllocationRatio orders two subgroups by their allocation ratio (allocated/threshold).
// Subgroups below their threshold are prioritized; optional ones (threshold=0) are deprioritized.
func orderByAllocationRatio(lCount, lThreshold, rCount, rThreshold int) int {
	lGangSatisfied := lCount >= lThreshold
	rGangSatisfied := rCount >= rThreshold

	// Prioritize the subgroup below its threshold
	if !lGangSatisfied && !rGangSatisfied {
		return equalPrioritization
	}
	if !lGangSatisfied {
		return lPrioritized
	}
	if !rGangSatisfied {
		return rPrioritized
	}

	lThresholdF := float64(lThreshold)
	rThresholdF := float64(rThreshold)

	// Prioritize required (threshold > 0) over optional (threshold == 0)
	if lThresholdF == 0 && rThresholdF > 0 {
		return rPrioritized
	}
	if rThresholdF == 0 && lThresholdF > 0 {
		return lPrioritized
	}
	// Both optional: prioritize the one with fewer allocated
	if lThresholdF == 0 && rThresholdF == 0 {
		if lCount < rCount {
			return lPrioritized
		}
		if rCount < lCount {
			return rPrioritized
		}
		return equalPrioritization
	}

	// Both satisfied: prioritize the one with the lower allocation ratio
	lRatio := float64(lCount) / lThresholdF
	rRatio := float64(rCount) / rThresholdF
	if lRatio < rRatio {
		return lPrioritized
	}
	if rRatio < lRatio {
		return rPrioritized
	}
	return equalPrioritization
}

func (sgop *subGroupOrderPlugin) OnSessionClose(_ *framework.Session) {}
