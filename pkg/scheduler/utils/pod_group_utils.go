// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"strconv"

	enginev2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
)

const (
	NoSchedulingBackoff     = -1
	SingleSchedulingBackoff = 1

	DefaultMarkUnschedulable = true
	DefaultSchedulingBackoff = NoSchedulingBackoff
)

func GetSchedulingBackoffValue(schedulingBackoff *int32) int32 {
	if schedulingBackoff == nil {
		return DefaultSchedulingBackoff
	}
	return *schedulingBackoff
}

func GetLastSchedulingCondition(podGroup *enginev2alpha2.PodGroup) *enginev2alpha2.SchedulingCondition {
	var lastCondition *enginev2alpha2.SchedulingCondition
	for _, condition := range podGroup.Status.SchedulingConditions {
		if lastCondition == nil {
			lastCondition = &condition
			continue
		}

		lastConditionID, err := strconv.Atoi(lastCondition.TransitionID)
		if err != nil {
			lastConditionID = -1
		}

		conditionID, err := strconv.Atoi(condition.TransitionID)
		if err != nil {
			conditionID = -1
		}
		if conditionID > lastConditionID {
			lastCondition = &condition
		}
	}
	return lastCondition
}

// GetSchedulingConditionIndex returns the index of the scheduling condition for the given nodepool, or -1 if it doesn't exist.
func GetSchedulingConditionIndex(podGroup *enginev2alpha2.PodGroup, nodepool string) int {
	for i, condition := range podGroup.Status.SchedulingConditions {
		if condition.NodePool == nodepool {
			return i
		}
	}

	return -1
}

func GetMarkUnschedulableValue(markUnschedulable *bool) bool {
	if markUnschedulable == nil {
		return DefaultMarkUnschedulable
	}
	return *markUnschedulable
}
