// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// SumGpuAllocation returns the total GPU count in a queue-status ResourceList: "*gpu"-suffixed extended
// resources across vendors (including GPU-sharing fractions), MIG profiles as GPU slices, and DRA device
// counts under a slash-free GPU DeviceClass name (which excludes "*gpu.memory" and similar).
func SumGpuAllocation(list v1.ResourceList) float64 {
	var total float64
	for name, quantity := range list {
		n := string(name)
		switch {
		case IsMigResource(n):
			slices, err := migGpuSlices(n)
			if err != nil {
				continue
			}
			total += float64(slices) * quantity.AsApproximateFloat64()
		case strings.HasSuffix(n, constants.GpuResource):
			total += quantity.AsApproximateFloat64()
		case !strings.Contains(n, "/") && IsGPUDeviceClass(n):
			total += quantity.AsApproximateFloat64()
		}
	}
	return total
}
