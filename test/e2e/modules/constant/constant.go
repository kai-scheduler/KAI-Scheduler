/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package constant

const (
	NvidiaGPUResource               = "nvidia.com/gpu"
	NvidiaGPUMemoryLabelName        = "nvidia.com/gpu.memory"
	NodeNamePodLabelName            = "kubernetes.io/hostname"
	SchedulerName                   = "kai-scheduler"
	SystemPodsNamespace             = "kai-scheduler"
	NonPreemptiblePriorityThreshold = 100
	EngineTestPodsApp               = "engine-e2e"

	SchedulerDeploymentName = "kai-scheduler-default"
	SchedulerContainerName  = "scheduler"
	AppLabelName            = "app"
	KaiReservationNamespace = "kai-resource-reservation"

	GPUDeviceClassName = "gpu.nvidia.com"
)
