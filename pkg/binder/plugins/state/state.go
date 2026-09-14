// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package state

type BindingState struct {
	BindingPodAnnotations map[string]string // Annotations that will be added to the pod after binding.
	ReservedGPUIds        []string
}
