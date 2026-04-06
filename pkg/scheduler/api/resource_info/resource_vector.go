// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"fmt"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info/resources"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type ResourceVector []float64

type ResourceVectorMap struct {
	resourceNames []v1.ResourceName
	namesToIndex  map[v1.ResourceName]int
}

// Core resource indices in vectors created by NewResourceVectorMap.
// These indices are fixed because NewResourceVectorMap always adds core resources first.
const (
	CPUIndex    = 0
	MemoryIndex = 1
	GPUIndex    = 2
	PodsIndex   = 3
)

// NewResourceVectorMap creates a new ResourceVectorMap initialized with base resources.
func NewResourceVectorMap() *ResourceVectorMap {
	result := &ResourceVectorMap{
		resourceNames: []v1.ResourceName{},
		namesToIndex:  make(map[v1.ResourceName]int),
	}

	// Add core resources first to ensure consistent ordering
	coreResources := []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory, v1.ResourceName(constants.GpuResource), v1.ResourcePods}
	for _, resourceName := range coreResources {
		result.AddResource(resourceName)
	}

	return result
}

func NewResourceVector(indexMap *ResourceVectorMap) ResourceVector {
	return make(ResourceVector, len(indexMap.resourceNames))
}

func NewSingleGpuVector(indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)
	vec.Set(GPUIndex, 1)
	return vec
}

// NewResourceVectorWithValues is a convenience function for tests only.
func NewResourceVectorWithValues(milliCPU, memory, gpus float64, indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)
	vec.Set(CPUIndex, milliCPU)
	vec.Set(MemoryIndex, memory)
	vec.Set(GPUIndex, gpus)
	return vec
}

func NewResourceVectorFromResourceList(resourceList v1.ResourceList, indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	for resourceName, rQuant := range resourceList {
		idx := indexMap.GetIndex(resourceName)
		if idx >= 0 {
			vec[idx] += convertResourceToFloat64(resourceName, rQuant)
		}
	}

	return vec
}

func (v *ResourceVector) Add(other ResourceVector) {
	if len(*v) < len(other) {
		extended := make(ResourceVector, len(other))
		copy(extended, *v)
		*v = extended
	}
	for i := range other {
		(*v)[i] += other[i]
	}
}

func (v *ResourceVector) Sub(other ResourceVector) {
	if len(*v) < len(other) {
		extended := make(ResourceVector, len(other))
		copy(extended, *v)
		*v = extended
	}
	for i := range other {
		(*v)[i] -= other[i]
	}
}

func (v ResourceVector) Clone() ResourceVector {
	cloned := make(ResourceVector, len(v))
	copy(cloned, v)
	return cloned
}

func (v ResourceVector) LessEqual(other ResourceVector) bool {
	minLen := min(len(v), len(other))
	for i := 0; i < minLen; i++ {
		if v[i] > other[i] {
			return false
		}
	}
	// v's extras must be <= 0 (compared against implicit 0 in other)
	for i := minLen; i < len(v); i++ {
		if v[i] > 0 {
			return false
		}
	}
	// other's extras must be >= 0 (implicit 0 in v must be <= them)
	for i := minLen; i < len(other); i++ {
		if other[i] < 0 {
			return false
		}
	}
	return true
}

func (v ResourceVector) Get(index int) float64 {
	if index < 0 || index >= len(v) {
		return 0
	}
	return v[index]
}

func (v ResourceVector) Set(index int, value float64) {
	if index >= 0 && index < len(v) {
		v[index] = value
	}
}

func (m *ResourceVectorMap) GetIndex(resourceName v1.ResourceName) int {
	normalized := normalizeResourceName(resourceName)
	if idx, exists := m.namesToIndex[normalized]; exists {
		return idx
	}
	return -1
}

func (m *ResourceVectorMap) AddResource(resourceName v1.ResourceName) {
	resourceName = normalizeResourceName(resourceName)
	if _, exists := m.namesToIndex[resourceName]; exists {
		return
	}
	m.namesToIndex[resourceName] = len(m.resourceNames)
	m.resourceNames = append(m.resourceNames, resourceName)
}

func (m *ResourceVectorMap) AddResourceList(resourceList v1.ResourceList) {
	for resourceName := range resourceList {
		m.AddResource(resourceName)
	}
}

func (m *ResourceVectorMap) Len() int {
	return len(m.resourceNames)
}

func (m *ResourceVectorMap) ResourceAt(index int) v1.ResourceName {
	if index < 0 || index >= len(m.resourceNames) {
		return ""
	}
	return m.resourceNames[index]
}

func BuildResourceVectorMap(nodeResources []v1.ResourceList) *ResourceVectorMap {
	result := NewResourceVectorMap()
	for _, rList := range nodeResources {
		result.AddResourceList(rList)
	}
	return result
}

func convertResourceToFloat64(rName v1.ResourceName, rQuant resource.Quantity) float64 {
	if rName == v1.ResourceCPU {
		return float64(rQuant.MilliValue())
	}
	return float64(rQuant.Value())
}

func isGpuResource(resourceName v1.ResourceName) bool {
	return strings.HasSuffix(string(resourceName), constants.GpuResource)
}

func normalizeResourceName(resourceName v1.ResourceName) v1.ResourceName {
	if isGpuResource(resourceName) {
		return v1.ResourceName(constants.GpuResource)
	}
	return resourceName
}

func (r *Resource) ToVector(indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	vec.Set(CPUIndex, r.milliCpu)
	vec.Set(MemoryIndex, r.memory)
	vec.Set(GPUIndex, r.gpus)

	for name, val := range r.scalarResources {
		if idx := indexMap.GetIndex(name); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	return vec
}

func (r *Resource) FromVector(vec ResourceVector, indexMap *ResourceVectorMap) {
	r.milliCpu = vec.Get(CPUIndex)
	r.memory = vec.Get(MemoryIndex)
	r.gpus = vec.Get(GPUIndex)
}

func (r *ResourceRequirements) ToVector(indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	vec.Set(CPUIndex, r.milliCpu)
	vec.Set(MemoryIndex, r.memory)
	vec.Set(GPUIndex, r.GPUs()+float64(r.GetDraGpusCount()))

	for name, val := range r.scalarResources {
		if idx := indexMap.GetIndex(name); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	for name, val := range r.MigResources() {
		if idx := indexMap.GetIndex(name); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	return vec
}

func (r *ResourceRequirements) FromVector(vec ResourceVector, indexMap *ResourceVectorMap) {
	r.milliCpu = vec.Get(CPUIndex)
	r.memory = vec.Get(MemoryIndex)

	gpuVal := vec.Get(GPUIndex)
	if gpuVal >= 1 {
		r.GpuResourceRequirement = *NewGpuResourceRequirementWithGpus(gpuVal, 0)
	} else if gpuVal > 0 {
		r.GpuResourceRequirement = *NewGpuResourceRequirement()
		r.GpuResourceRequirement.portion = gpuVal
		r.GpuResourceRequirement.count = 1
	}
}

func (v ResourceVector) SetMax(other ResourceVector) {
	for i := range min(len(v), len(other)) {
		if other[i] > v[i] {
			v[i] = other[i]
		}
	}
}

func (v ResourceVector) TotalGPUs(indexMap *ResourceVectorMap) float64 {
	total := v.Get(GPUIndex)
	for i := range indexMap.Len() {
		name := indexMap.ResourceAt(i)
		if !IsMigResource(name) {
			continue
		}
		gpuPortion, _, err := resources.ExtractGpuAndMemoryFromMigResourceName(string(name))
		if err != nil {
			continue
		}
		total += float64(gpuPortion) * v.Get(i)
	}
	return total
}

func (v ResourceVector) IsZero() bool {
	for _, val := range v {
		if val != 0 {
			return false
		}
	}
	return true
}

func DetailedResourceString(vec ResourceVector, gpuReq *GpuResourceRequirement, m *ResourceVectorMap) string {
	messageBuilder := strings.Builder{}
	messageBuilder.WriteString(fmt.Sprintf(
		"GPU: %s, CPU: %s (cores), memory: %s (GB)",
		HumanizeResource(gpuReq.GetGpusQuota(), 1),
		HumanizeResource(vec.Get(CPUIndex), MilliCPUToCores),
		HumanizeResource(vec.Get(MemoryIndex), MemoryToGB),
	))

	for i := 0; i < m.Len(); i++ {
		name := m.ResourceAt(i)
		if name == v1.ResourceCPU || name == v1.ResourceMemory || name == v1.ResourceName(constants.GpuResource) {
			continue
		}
		if IsMigResource(name) {
			continue
		}
		val := vec.Get(i)
		if val == 0 {
			continue
		}
		if name == v1.ResourceEphemeralStorage || name == v1.ResourceStorage {
			val = val / MemoryToGB
			messageBuilder.WriteString(fmt.Sprintf(", %s: %s (GB)", name, HumanizeResource(val, 1)))
		} else {
			messageBuilder.WriteString(fmt.Sprintf(", %s: %s", name, HumanizeResource(val, 1)))
		}
	}
	for migName, migQuant := range gpuReq.MigResources() {
		messageBuilder.WriteString(fmt.Sprintf(", mig %s: %d", migName, migQuant))
	}
	return messageBuilder.String()
}

func (v ResourceVector) ToResourceQuantities(indexMap *ResourceVectorMap) map[v1.ResourceName]float64 {
	result := make(map[v1.ResourceName]float64, indexMap.Len())
	for i := 0; i < indexMap.Len(); i++ {
		name := indexMap.ResourceAt(i)
		result[name] = v.Get(i)
	}
	return result
}
