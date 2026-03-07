// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

const (
	cpu    = float64(111)
	memory = float64(22)
	gpu    = float64(0.5)
)

func TestNewResourceQuantities(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	asssertResourceQuantity(t, rq)
}

func TestEmptyResourceQuantities(t *testing.T) {
	rq := EmptyResourceQuantities()
	for _, resource := range AllResources {
		assert.Zero(t, rq[resource])
	}
}

func TestResourceQuantitiesClone(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	clone := rq.Clone()
	asssertResourceQuantity(t, clone)
}

func TestResourceQuantitiesCloneAndModify(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	clone := rq.Clone()
	rq[MemoryResource] = 3
	assert.Equal(t, float64(3), rq[MemoryResource])
	assert.Equal(t, memory, clone[MemoryResource])
}

func TestResourceQuantitiesAdd(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(1, 2, 3)
	rq.Add(rq2)
	assert.Equal(t, cpu+1, rq[CpuResource])
	assert.Equal(t, memory+2, rq[MemoryResource])
	assert.Equal(t, gpu+3, rq[GpuResource])
}

func TestResourceQuantitiesSub(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(3, 2, 1)
	rq.Sub(rq2)
	assert.Equal(t, cpu-3, rq[CpuResource])
	assert.Equal(t, memory-2, rq[MemoryResource])
	assert.Equal(t, gpu-1, rq[GpuResource])
}

func TestResourceQuantitiesLess(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu+1, memory+1, gpu+0.1)
	assert.True(t, rq.Less(rq2))
}

func TestResourceQuantitiesNotLess(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu+1, memory, gpu+0.1)
	assert.False(t, rq.Less(rq2))
}

func TestResourceQuantitiesLessEqual(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu+1, memory+1, gpu)
	assert.True(t, rq.LessEqual(rq2))
}

func TestResourceQuantitiesNotLessEqual(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu+1, memory+1, gpu-0.1)
	assert.False(t, rq.LessEqual(rq2))
}

func TestResourceQuantitiesLessEqualInOneResource(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu+1, memory, gpu-0.1)
	assert.True(t, rq.LessInAtLeastOneResource(rq2))
}

func TestResourceQuantitiesNotLessEqualInOneResource(t *testing.T) {
	rq := NewResourceQuantities(cpu, memory, gpu)
	rq2 := NewResourceQuantities(cpu, memory-1, gpu-0.1)
	assert.False(t, rq.LessInAtLeastOneResource(rq2))
}

func asssertResourceQuantity(t *testing.T, rq ResourceQuantities) {
	assert.Equal(t, cpu, rq[CpuResource])
	assert.Equal(t, memory, rq[MemoryResource])
	assert.Equal(t, gpu, rq[GpuResource])
}

func TestCompareResources(t *testing.T) {
	for testCase, testData := range map[string]struct {
		resource1 float64
		resource2 float64
		expected  int
	}{
		"sanity smaller": {
			resource1: 1.5,
			resource2: 2.5,
			expected:  -1,
		},
		"sanity bigger": {
			resource1: 2.5,
			resource2: 1.5,
			expected:  1,
		},
		"equal": {
			resource1: 2.5,
			resource2: 2.5,
			expected:  0,
		},
		"unlimited vs number": {
			resource1: commonconstants.UnlimitedResourceQuantity,
			resource2: 2.5,
			expected:  1,
		},
		"unlimited vs number other direction": {
			resource1: 2.5,
			resource2: commonconstants.UnlimitedResourceQuantity,
			expected:  -1,
		},
		"unlimited vs unlimited": {
			resource1: commonconstants.UnlimitedResourceQuantity,
			resource2: commonconstants.UnlimitedResourceQuantity,
			expected:  0,
		},
	} {
		t.Run(testCase, func(t *testing.T) {
			assert.Equal(t, testData.expected, compareQuantities(testData.resource1, testData.resource2))
		})
	}
}
