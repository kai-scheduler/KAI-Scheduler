// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// gpuMemorySuffix identifies GPU-memory resources (e.g. nvidia.com/gpu.memory, run.ai/gpu.memory),
// which report memory rather than a GPU count and must not be summed as GPUs.
const gpuMemorySuffix = "gpu.memory"

// SumGpuAllocation returns the total number of GPUs represented by a ResourceList: whole and
// fractional extended GPUs (any "*gpu"-suffixed resource such as nvidia.com/gpu or amd.com/gpu),
// DRA GPU device counts (keyed by the GPU DeviceClass name, e.g. gpu.nvidia.com), and MIG profiles
// (nvidia.com/mig-<slices>g.<mem>gb, counted as <slices> GPU portions each, matching the scheduler's
// MIG accounting). GPU memory (any "*gpu.memory" resource) is excluded, since it reports memory
// rather than a GPU count. Extended GPUs are matched by the same "gpu" suffix the queue metrics use,
// so vendors beyond NVIDIA/AMD are counted too.
func SumGpuAllocation(list v1.ResourceList) float64 {
	var total float64
	for name, quantity := range list {
		n := string(name)
		switch {
		case strings.HasSuffix(n, gpuMemorySuffix):
			continue
		case IsMigResource(n):
			gpuPortion, _, err := ExtractGpuAndMemoryFromMigResourceName(n)
			if err != nil {
				continue
			}
			total += float64(gpuPortion) * quantity.AsApproximateFloat64()
		case strings.HasSuffix(n, constants.GpuResource):
			total += quantity.AsApproximateFloat64()
		case IsGPUDeviceClass(n):
			total += quantity.AsApproximateFloat64()
		}
	}
	return total
}
