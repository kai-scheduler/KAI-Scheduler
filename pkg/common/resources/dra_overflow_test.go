// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"math"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func gpuClaimWithCount(count int64) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{{
					Name: "gpu",
					Exactly: &resourceapi.ExactDeviceRequest{
						DeviceClassName: "gpu.nvidia.com",
						AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
						Count:           count,
					},
				}},
			},
		},
	}
}

// Two API-valid ResourceClaims, each requesting Count=MaxInt64 of the same GPU
// device class, must not wrap the aggregated device count negative. The sum
// saturates at MaxInt64 instead, so the request stays a (very large) positive
// count rather than corrupting the queue's GPU accounting.
func TestExtractDRAGPUResourcesFromClaims_saturatesOnOverflow(t *testing.T) {
	claims := []*resourceapi.ResourceClaim{
		gpuClaimWithCount(math.MaxInt64),
		gpuClaimWithCount(math.MaxInt64),
	}
	got := ExtractDRAGPUResourcesFromClaims(claims)
	if c := got["gpu.nvidia.com"]; c != math.MaxInt64 {
		t.Fatalf("aggregated gpu.nvidia.com count = %d, want %d (saturated, non-negative)", c, int64(math.MaxInt64))
	}

	// Baseline: normal counts still sum exactly.
	base := ExtractDRAGPUResourcesFromClaims([]*resourceapi.ResourceClaim{
		gpuClaimWithCount(2), gpuClaimWithCount(3),
	})
	if c := base["gpu.nvidia.com"]; c != 5 {
		t.Fatalf("aggregated gpu.nvidia.com count = %d, want 5", c)
	}
}

// A single API-valid ResourceClaim can mix allocation modes: an ExactCount
// request at math.MaxInt64 alongside an AllocationModeAll request (the apiserver
// allows any positive ExactCount and requires All to carry no count). The "All"
// branch adds a conservative +1 for bookkeeping, which must saturate rather than
// wrap the already-maxed per-claim total to MinInt64. A negative total would then
// be dropped by the >0 filter in ExtractDRAGPUResourcesFromClaims, silently
// undercounting the oversized claim to 0.
func TestCountGPUDevicesFromClaim_ExactMaxThenAllSaturates(t *testing.T) {
	claim := &resourceapi.ResourceClaim{
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: "exact",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: "gpu.nvidia.com",
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           math.MaxInt64,
						},
					},
					{
						Name: "all",
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: "gpu.nvidia.com",
							AllocationMode:  resourceapi.DeviceAllocationModeAll,
						},
					},
				},
			},
		},
	}
	if got := countGPUDevicesFromClaim(claim); got != math.MaxInt64 {
		t.Fatalf("countGPUDevicesFromClaim = %d, want %d (saturated); a negative result is dropped by the >0 filter and undercounts the claim to 0", got, int64(math.MaxInt64))
	}

	// The oversized claim must be counted (as a large positive), not dropped.
	if c := ExtractDRAGPUResourcesFromClaims([]*resourceapi.ResourceClaim{claim})["gpu.nvidia.com"]; c != math.MaxInt64 {
		t.Fatalf("aggregated gpu.nvidia.com count = %d, want %d (claim must not be dropped)", c, int64(math.MaxInt64))
	}
}
