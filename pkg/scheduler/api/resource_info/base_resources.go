// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"github.com/dustin/go-humanize"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

const (
	minMilliCPU             float64 = 10
	minMilliScalarResources int64   = 10
	MinMemory               float64 = 10 * 1024 * 1024
	MilliCPUToCores         float64 = 1000
	MemoryToGB              float64 = 1000 * 1000 * 1000
)

type BaseResource struct {
	milliCpu        float64
	memory          float64
	scalarResources map[v1.ResourceName]int64
}

func EmptyBaseResource() *BaseResource {
	return &BaseResource{
		milliCpu:        0,
		memory:          0,
		scalarResources: make(map[v1.ResourceName]int64),
	}
}

func NewBaseResourceWithValues(milliCPU float64, memory float64) *BaseResource {
	return &BaseResource{
		milliCpu:        milliCPU,
		memory:          memory,
		scalarResources: make(map[v1.ResourceName]int64),
	}
}

func (r *BaseResource) Clone() *BaseResource {
	return &BaseResource{
		milliCpu:        r.milliCpu,
		memory:          r.memory,
		scalarResources: maps.Clone(r.scalarResources),
	}
}

func (r *BaseResource) Add(other *BaseResource) {
	r.milliCpu += other.milliCpu
	r.memory += other.memory
	for rName, rrValue := range other.scalarResources {
		r.scalarResources[rName] += rrValue
		if r.scalarResources[rName] == 0 {
			delete(r.scalarResources, rName)
		}
	}
}

func (r *BaseResource) Sub(other *BaseResource) {
	r.milliCpu -= other.milliCpu
	r.memory -= other.memory
	for rName, rrValue := range other.scalarResources {
		r.scalarResources[rName] -= rrValue
		if r.scalarResources[rName] == 0 {
			delete(r.scalarResources, rName)
		}
	}
}

func (r *BaseResource) Get(rn v1.ResourceName) float64 {
	switch rn {
	case v1.ResourceCPU:
		return r.milliCpu
	case v1.ResourceMemory:
		return r.memory
	default:
		rQuan, found := r.scalarResources[rn]
		if !found {
			return 0
		}
		return float64(rQuan)
	}
}

func (r *BaseResource) LessEqual(rr *BaseResource) bool {
	if r.milliCpu > rr.milliCpu {
		return false
	}
	if r.memory > rr.memory {
		return false
	}
	for rName, rQuant := range r.scalarResources {
		rrQuant, found := rr.scalarResources[rName]
		if !found || rQuant > rrQuant {
			return false
		}
	}

	return true
}

func (r *BaseResource) ToResourceList() v1.ResourceList {
	rl := v1.ResourceList{}

	rl[v1.ResourceCPU] = *resource.NewMilliQuantity(int64(r.milliCpu), resource.DecimalSI)
	rl[v1.ResourceMemory] = *resource.NewQuantity(int64(r.memory), resource.DecimalSI)
	for rName, rQuant := range r.scalarResources {
		rl[rName] = *resource.NewMilliQuantity(int64(rQuant), resource.DecimalSI)
	}

	return rl
}

func (r *BaseResource) IsEmpty() bool {
	if r.milliCpu >= minMilliCPU || r.memory >= MinMemory {
		return false
	}
	for _, rQuant := range r.scalarResources {
		if rQuant >= minMilliScalarResources {
			return false
		}
	}

	return true
}

func (r *BaseResource) SetMaxResource(rr *BaseResource) {
	if r == nil || rr == nil {
		return
	}

	if rr.milliCpu > r.milliCpu {
		r.milliCpu = rr.milliCpu
	}
	if rr.memory > r.memory {
		r.memory = rr.memory
	}

	if r.scalarResources == nil {
		r.scalarResources = make(map[v1.ResourceName]int64)
	}
	for rrName, rrQuant := range rr.scalarResources {
		if rQuant, found := r.scalarResources[rrName]; !found || rrQuant > rQuant {
			r.scalarResources[rrName] = rrQuant
		}
	}
}

func (r *BaseResource) Cpu() float64 {
	return r.Get(v1.ResourceCPU)
}

func (r *BaseResource) Memory() float64 {
	return r.Get(v1.ResourceMemory)
}

func (r *BaseResource) ScalarResources() map[v1.ResourceName]int64 {
	return r.scalarResources
}

func HumanizeResource(value float64, unitAdjustment float64) string {
	if value == commonconstants.UnlimitedResourceQuantity {
		return "Unlimited"
	}
	return humanize.FtoaWithDigits(value/unitAdjustment, 3)
}
