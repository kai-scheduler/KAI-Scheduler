// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// SumGpuAllocation returns the total number of GPUs represented by a ResourceList, matching how KAI
// records GPU allocation in a queue's status:
//   - extended GPUs: any "*gpu"-suffixed resource (e.g. nvidia.com/gpu, amd.com/gpu), including
//     fractional GPU-sharing values, summed across vendors;
//   - DRA GPUs: device counts written under the GPU DeviceClass name (e.g. gpu.nvidia.com). DeviceClass
//     names are Kubernetes object names and never contain "/", which distinguishes them from ordinary
//     domain-qualified extended resources;
//   - MIG: nvidia.com/mig-<slices>g.<mem>gb, counted as <slices> GPU portions each, matching the
//     scheduler's MIG accounting.
//
// Domain-qualified extended resources that merely contain "gpu" in their name but are not a GPU count
// (e.g. nvidia.com/gpu.memory, run.ai/gpu.memory, volcano.sh/vgpu-memory) are not counted: they carry a
// "/" and match neither the "gpu" suffix nor the slash-free DeviceClass rule.
func SumGpuAllocation(list v1.ResourceList) float64 {
	var total float64
	for name, quantity := range list {
		n := string(name)
		switch {
		case IsMigResource(n):
			gpuPortion, _, err := ExtractGpuAndMemoryFromMigResourceName(n)
			if err != nil {
				continue
			}
			total += float64(gpuPortion) * quantity.AsApproximateFloat64()
		case strings.HasSuffix(n, constants.GpuResource):
			total += quantity.AsApproximateFloat64()
		case !strings.Contains(n, "/") && IsGPUDeviceClass(n):
			total += quantity.AsApproximateFloat64()
		}
	}
	return total
}
