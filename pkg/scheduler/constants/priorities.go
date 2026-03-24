// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package constants

const (
	// PriorityInferenceNumber - used for inference workloads that aren't preemptible
	PriorityInferenceNumber = 125

	// PriorityBuildNumber (Build/Interactive) - used for workloads that aren't preemptible
	PriorityBuildNumber = 100

	// PriorityInteractivePreemptibleNumber - used for interactive workloads that are preemptible
	PriorityInteractivePreemptibleNumber = 75

	// PriorityTrainNumber - used for batch jobs and training workloads that are preemptible
	PriorityTrainNumber = 50
)
