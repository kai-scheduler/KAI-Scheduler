// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"
)

type ResourceInterface interface {
	Cpu() float64
	Memory() float64
	GPUs() float64
	GpuMemory() int64
	MigResources() map[v1.ResourceName]int64
	ScalarResources() map[v1.ResourceName]int64
	GpusAsString() string
}
