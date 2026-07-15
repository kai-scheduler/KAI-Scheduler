// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const (
	CpuResource    ResourceName = "CPU"
	MemoryResource ResourceName = "Memory"
	GpuResource    ResourceName = "GPU"
)

type ResourceName string
type ResourceQuantities map[ResourceName]float64

var AllResources = []ResourceName{CpuResource, MemoryResource, GpuResource}

func NewResourceQuantities(cpuQty, memoryQty, gpuQty float64) ResourceQuantities {
	return ResourceQuantities{
		CpuResource:    cpuQty,
		MemoryResource: memoryQty,
		GpuResource:    gpuQty,
	}
}

func EmptyResourceQuantities() ResourceQuantities {
	return NewResourceQuantities(0, 0, 0)
}

func (rq ResourceQuantities) Clone() ResourceQuantities {
	return NewResourceQuantities(rq[CpuResource], rq[MemoryResource], rq[GpuResource])
}

func (rq ResourceQuantities) Add(other ResourceQuantities) {
	for _, resource := range AllResources {
		rq[resource] += other[resource]
	}
}

func (rq ResourceQuantities) Sub(other ResourceQuantities) {
	for _, resource := range AllResources {
		rq[resource] -= other[resource]
	}
}

func (rq ResourceQuantities) Less(other ResourceQuantities) bool {
	for _, resource := range AllResources {
		if rq[resource] >= other[resource] {
			return false
		}
	}
	return true
}

func (rq ResourceQuantities) LessEqual(other ResourceQuantities) bool {
	for _, resource := range AllResources {
		if compareQuantities(rq[resource], other[resource]) > 0 {
			return false
		}
	}
	return true
}

func (rq ResourceQuantities) LessInAtLeastOneResource(other ResourceQuantities) bool {
	return !other.LessEqual(rq)
}

func (rq ResourceQuantities) String() string {
	return resource_info.NewResource(
		rq[CpuResource], rq[MemoryResource], rq[GpuResource],
	).String()
}

func compareQuantities(quantity, other float64) int {
	if quantity == commonconstants.UnlimitedResourceQuantity {
		if other == commonconstants.UnlimitedResourceQuantity {
			return 0
		}
		return 1
	}

	if other == commonconstants.UnlimitedResourceQuantity {
		return -1
	}

	if quantity > other {
		return 1
	}
	if quantity < other {
		return -1
	}
	return 0
}
