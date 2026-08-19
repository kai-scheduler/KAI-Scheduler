// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	v1 "k8s.io/api/core/v1"
)

func IsAllocated(pod *v1.Pod) bool {
	if pod.Status.Phase == v1.PodPending {
		return isScheduled(pod)
	}
	return pod.Status.Phase == v1.PodRunning
}

func isScheduled(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodScheduled {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}
