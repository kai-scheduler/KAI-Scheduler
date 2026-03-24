// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consts

const (
	SharedGpuPodName                = "shared-gpu-pod-name"
	SharedGpuPodNamespace           = "shared-gpu-pod-namespace"
	ScalingPodContainerName         = "scaling-pod"
	DefaultCoolDownSeconds          = 60
	DefaultScalingPodImageName      = "scaling-pod"
	DefaultScalingPodImage          = "registry/local/kai-scheduler/scalingpod" + DefaultScalingPodImageName
	DefaultGPUMemoryToFractionRatio = 0.1
)
