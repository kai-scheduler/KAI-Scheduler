// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

func IsKaiUtilityPod(pod *v1.Pod) bool {
	return IsResourceReservationTask(pod) || IsScaleAdjustTask(pod)
}

func IsScaleAdjustTask(pod *v1.Pod) bool {
	appName, found := pod.Labels[commonconstants.AppLabelName]
	if !found {
		return false
	}
	config := conf.GetConfig()
	return appName == config.ScalingPodAppLabelValue
}

func IsResourceReservationTask(pod *v1.Pod) bool {
	appName, found := pod.Labels[commonconstants.AppLabelName]
	if !found {
		return false
	}
	config := conf.GetConfig()
	return appName == config.ResourceReservationAppLabelValue
}
