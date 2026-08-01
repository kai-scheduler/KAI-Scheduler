// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestQueueResourceShare_ResourceShare(t *testing.T) {
	qrs := createQueueResourceShare()
	memoryResourceShare := qrs.ResourceShare(MemoryResource)
	assert.Equal(t, qrs.Memory.Deserved, memoryResourceShare.Deserved)
	assert.Equal(t, qrs.Memory.FairShare, memoryResourceShare.FairShare)
	assert.Equal(t, qrs.Memory.MaxAllowed, memoryResourceShare.MaxAllowed)
	assert.Equal(t, qrs.Memory.OverQuotaWeight, memoryResourceShare.OverQuotaWeight)
	assert.Equal(t, qrs.Memory.Allocated, memoryResourceShare.Allocated)
	assert.Equal(t, qrs.Memory.AllocatedNotPreemptible, memoryResourceShare.AllocatedNotPreemptible)
	assert.Equal(t, qrs.Memory.Request, memoryResourceShare.Request)
}

func TestQueueResourceShare_ResourceShare_Modify(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.ResourceShare(MemoryResource)
	qrs.Memory.Deserved = 123
	assert.Equal(t, qrs.Memory.Deserved, share.Deserved)
}

func TestQueueResourceShare_ResourceShareUnknownResource(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.ResourceShare("unknown")
	assert.Nil(t, share)
}

func TestQueueResourceShare_GetAllocatableShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetAllocatableShare()
	assert.Equal(t, float64(2), share[CpuResource])
	assert.Equal(t, float64(9), share[MemoryResource])
	assert.Equal(t, float64(16), share[GpuResource])
}

func TestQueueResourceShare_GetFairShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetFairShare()
	assert.Equal(t, float64(2), share[CpuResource])
	assert.Equal(t, float64(9), share[MemoryResource])
	assert.Equal(t, float64(16), share[GpuResource])
}

func TestQueueResourceShare_GetDeservedShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetDeservedShare()
	assert.Equal(t, float64(1), share[CpuResource])
	assert.Equal(t, float64(8), share[MemoryResource])
	assert.Equal(t, float64(15), share[GpuResource])
}

func TestQueueResourceShare_GetAllocatedShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetAllocatedShare()
	assert.Equal(t, float64(5), share[CpuResource])
	assert.Equal(t, float64(12), share[MemoryResource])
	assert.Equal(t, float64(19), share[GpuResource])
}

func TestQueueResourceShare_GetAllocatedNonPreemptible(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetAllocatedNonPreemptible()
	assert.Equal(t, float64(6), share[CpuResource])
	assert.Equal(t, float64(13), share[MemoryResource])
	assert.Equal(t, float64(20), share[GpuResource])
}

func TestQueueResourceShare_GetMaxAllowedShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetMaxAllowedShare()
	assert.Equal(t, float64(3), share[CpuResource])
	assert.Equal(t, float64(10), share[MemoryResource])
	assert.Equal(t, float64(17), share[GpuResource])
}

func TestQueueResourceShare_GetRequestShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetRequestShare()
	assert.Equal(t, float64(7), share[CpuResource])
	assert.Equal(t, float64(14), share[MemoryResource])
	assert.Equal(t, float64(21), share[GpuResource])
}

func TestQueueResourceShare_GetRequestedShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetRequestableShare()
	assert.Equal(t, float64(3), share[CpuResource])
	assert.Equal(t, float64(10), share[MemoryResource])
	assert.Equal(t, float64(17), share[GpuResource])
}

func TestQueueResourceShare_GetDominantResourceShare(t *testing.T) {
	qrs := createQueueResourceShare()
	share := qrs.GetDominantResourceShare(ResourceQuantities{})
	assert.Equal(t, 2.5, share)
}

func TestQueueResourceShare_AddResourceShare(t *testing.T) {
	qrs := createQueueResourceShare()
	qrs.AddResourceShare(GpuResource, 1)
	assert.Equal(t, float64(17), qrs.GPU.FairShare)
}

func TestQueueResourceShare_SetQuotaResources(t *testing.T) {
	qrs := createQueueResourceShare()
	qrs.SetQuotaResources(CpuResource, 8.8, 9.9, 1.1)
	assert.Equal(t, 8.8, qrs.CPU.Deserved)
	assert.Equal(t, 9.9, qrs.CPU.MaxAllowed)
	assert.Equal(t, 1.1, qrs.CPU.OverQuotaWeight)
}

func TestQueueResourceShare_FairShareLessThanAllocated(t *testing.T) {
	tests := []struct {
		name     string
		share    QueueResourceShare
		expected bool
	}{
		{
			name: "allocated is above fair share in every resource",
			share: QueueResourceShare{
				CPU:    ResourceShare{FairShare: 1, Allocated: 2},
				Memory: ResourceShare{FairShare: 3, Allocated: 4},
				GPU:    ResourceShare{FairShare: 5, Allocated: 6},
			},
			expected: true,
		},
		{
			name: "one equal resource keeps the strict comparison false",
			share: QueueResourceShare{
				CPU:    ResourceShare{FairShare: 1, Allocated: 2},
				Memory: ResourceShare{FairShare: 3, Allocated: 3},
				GPU:    ResourceShare{FairShare: 5, Allocated: 6},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.share.FairShareLessThanAllocated())
		})
	}
}

func TestQueueResourceShare_AllocatedPlusResourcesLessEqualDeserved(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	resources := resource_info.NewResourceVectorWithValues(2, 3, 4, vectorMap)

	tests := []struct {
		name     string
		share    QueueResourceShare
		expected bool
	}{
		{
			name: "allocated plus resources fits exactly",
			share: QueueResourceShare{
				CPU:    ResourceShare{Allocated: 1, Deserved: 3},
				Memory: ResourceShare{Allocated: 2, Deserved: 5},
				GPU:    ResourceShare{Allocated: 3, Deserved: 7},
			},
			expected: true,
		},
		{
			name: "one resource exceeds deserved",
			share: QueueResourceShare{
				CPU:    ResourceShare{Allocated: 1, Deserved: 3},
				Memory: ResourceShare{Allocated: 3, Deserved: 5},
				GPU:    ResourceShare{Allocated: 3, Deserved: 7},
			},
			expected: false,
		},
		{
			name: "unlimited deserved accepts any quantity",
			share: QueueResourceShare{
				CPU:    ResourceShare{Allocated: 1, Deserved: 3},
				Memory: ResourceShare{Allocated: 2, Deserved: 5},
				GPU: ResourceShare{
					Allocated: 3,
					Deserved:  commonconstants.UnlimitedResourceQuantity,
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.share.AllocatedPlusResourcesLessEqualDeserved(resources, vectorMap))
		})
	}
}

func TestQueueResourceShare_AllocatedPlusNoResourcesLessEqualDeserved(t *testing.T) {
	share := QueueResourceShare{
		CPU:    ResourceShare{Allocated: 1, Deserved: 1},
		Memory: ResourceShare{Allocated: 2, Deserved: 2},
		GPU:    ResourceShare{Allocated: 3, Deserved: 3},
	}

	assert.True(t, share.AllocatedPlusResourcesLessEqualDeserved(nil, nil))
}

func TestQueueResourceShare_DirectComparisonsDoNotAllocate(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	resources := resource_info.NewResourceVectorWithValues(2, 3, 4, vectorMap)
	share := QueueResourceShare{
		CPU:    ResourceShare{FairShare: 1, Allocated: 2, Deserved: 4},
		Memory: ResourceShare{FairShare: 2, Allocated: 3, Deserved: 6},
		GPU:    ResourceShare{FairShare: 3, Allocated: 4, Deserved: 8},
	}
	var fairShareResult, deservedResult bool

	allocations := testing.AllocsPerRun(100, func() {
		fairShareResult = share.FairShareLessThanAllocated()
		deservedResult = share.AllocatedPlusResourcesLessEqualDeserved(resources, vectorMap)
	})

	assert.True(t, fairShareResult)
	assert.True(t, deservedResult)
	assert.Zero(t, allocations)
}

func createQueueResourceShare() *QueueResourceShare {
	return &QueueResourceShare{
		CPU: ResourceShare{
			Deserved:                1,
			FairShare:               2,
			MaxAllowed:              3,
			OverQuotaWeight:         4,
			Allocated:               5,
			AllocatedNotPreemptible: 6,
			Request:                 7,
		},
		Memory: ResourceShare{
			Deserved:                8,
			FairShare:               9,
			MaxAllowed:              10,
			OverQuotaWeight:         11,
			Allocated:               12,
			AllocatedNotPreemptible: 13,
			Request:                 14,
		},
		GPU: ResourceShare{
			Deserved:                15,
			FairShare:               16,
			MaxAllowed:              17,
			OverQuotaWeight:         18,
			Allocated:               19,
			AllocatedNotPreemptible: 20,
			Request:                 21,
		},
	}
}
