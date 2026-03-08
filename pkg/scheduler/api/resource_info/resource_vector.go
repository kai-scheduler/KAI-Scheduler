// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"fmt"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type ResourceVector []float64

type ResourceVectorMap struct {
	resourceNames []string
	namesToIndex  map[string]int
}

// NewResourceVectorMap creates a new ResourceVectorMap initialized with base resources.
func NewResourceVectorMap() *ResourceVectorMap {
	result := &ResourceVectorMap{
		resourceNames: []string{},
		namesToIndex:  make(map[string]int),
	}

	// Add core resources first to ensure consistent ordering
	coreResources := []string{string(v1.ResourceCPU), string(v1.ResourceMemory), constants.GpuResource, string(v1.ResourcePods)}
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
	gpuIdx := indexMap.GetIndex(constants.GpuResource)
	if gpuIdx >= 0 {
		vec.Set(gpuIdx, 1)
	}
	return vec
}

// NewResourceVectorWithValues is a convenience function for tests only.
func NewResourceVectorWithValues(milliCPU, memory, gpus float64, indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)
	cpuIdx := indexMap.GetIndex(string(v1.ResourceCPU))
	memIdx := indexMap.GetIndex(string(v1.ResourceMemory))
	gpuIdx := indexMap.GetIndex(constants.GpuResource)
	if cpuIdx < 0 || memIdx < 0 || gpuIdx < 0 {
		panic("resource vector map missing core resource indexes")
	}
	vec.Set(cpuIdx, milliCPU)
	vec.Set(memIdx, memory)
	vec.Set(gpuIdx, gpus)
	return vec
}

func NewResourceVectorFromResourceList(resourceList v1.ResourceList, indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	for resourceName, rQuant := range resourceList {
		normalizedName := normalizeResourceName(string(resourceName))
		idx := indexMap.GetIndex(normalizedName)
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

func (m *ResourceVectorMap) GetIndex(resourceName string) int {
	resourceName = normalizeResourceName(resourceName)
	if idx, exists := m.namesToIndex[resourceName]; exists {
		return idx
	}
	return -1
}

func (m *ResourceVectorMap) AddResource(resourceName string) {
	resourceName = normalizeResourceName(resourceName)
	if _, exists := m.namesToIndex[resourceName]; exists {
		return
	}
	m.namesToIndex[resourceName] = len(m.resourceNames)
	m.resourceNames = append(m.resourceNames, resourceName)
}

func (m *ResourceVectorMap) AddResourceList(resourceList v1.ResourceList) {
	for resourceName := range resourceList {
		m.AddResource(string(resourceName))
	}
}

func (m *ResourceVectorMap) Len() int {
	return len(m.resourceNames)
}

func (m *ResourceVectorMap) ResourceAt(index int) string {
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

func isGpuResource(resourceName string) bool {
	return strings.HasSuffix(resourceName, constants.GpuResource)
}

func normalizeResourceName(resourceName string) string {
	if isGpuResource(resourceName) {
		return constants.GpuResource
	}
	return resourceName
}

func (r *Resource) ToVector(indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	vec.Set(indexMap.GetIndex(string(v1.ResourceCPU)), r.milliCpu)
	vec.Set(indexMap.GetIndex(string(v1.ResourceMemory)), r.memory)
	vec.Set(indexMap.GetIndex(constants.GpuResource), r.gpus)

	for name, val := range r.scalarResources {
		if idx := indexMap.GetIndex(string(name)); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	return vec
}

func (r *Resource) FromVector(vec ResourceVector, indexMap *ResourceVectorMap) {
	r.milliCpu = vec.Get(indexMap.GetIndex(string(v1.ResourceCPU)))
	r.memory = vec.Get(indexMap.GetIndex(string(v1.ResourceMemory)))
	r.gpus = vec.Get(indexMap.GetIndex(constants.GpuResource))
}

func (r *ResourceRequirements) ToVector(indexMap *ResourceVectorMap) ResourceVector {
	vec := NewResourceVector(indexMap)

	vec.Set(indexMap.GetIndex(string(v1.ResourceCPU)), r.milliCpu)
	vec.Set(indexMap.GetIndex(string(v1.ResourceMemory)), r.memory)
	vec.Set(indexMap.GetIndex(constants.GpuResource), r.GPUs()+float64(r.GetDraGpusCount()))

	for name, val := range r.scalarResources {
		if idx := indexMap.GetIndex(string(name)); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	for name, val := range r.MigResources() {
		if idx := indexMap.GetIndex(string(name)); idx >= 0 {
			vec.Set(idx, float64(val))
		}
	}

	return vec
}

func (r *ResourceRequirements) FromVector(vec ResourceVector, indexMap *ResourceVectorMap) {
	r.milliCpu = vec.Get(indexMap.GetIndex(string(v1.ResourceCPU)))
	r.memory = vec.Get(indexMap.GetIndex(string(v1.ResourceMemory)))

	gpuVal := vec.Get(indexMap.GetIndex(constants.GpuResource))
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

func (v ResourceVector) IsZero() bool {
	for _, val := range v {
		if val != 0 {
			return false
		}
	}
	return true
}

func DetailedResourceString(vec ResourceVector, gpuReq *GpuResourceRequirement, m *ResourceVectorMap) string {
	cpuIdx := m.GetIndex(string(v1.ResourceCPU))
	memIdx := m.GetIndex(string(v1.ResourceMemory))

	messageBuilder := strings.Builder{}
	messageBuilder.WriteString(fmt.Sprintf(
		"GPU: %s, CPU: %s (cores), memory: %s (GB)",
		HumanizeResource(gpuReq.GetGpusQuota(), 1),
		HumanizeResource(vec.Get(cpuIdx), MilliCPUToCores),
		HumanizeResource(vec.Get(memIdx), MemoryToGB),
	))

	for i := 0; i < m.Len(); i++ {
		name := m.ResourceAt(i)
		if name == string(v1.ResourceCPU) || name == string(v1.ResourceMemory) || name == constants.GpuResource {
			continue
		}
		if IsMigResource(v1.ResourceName(name)) {
			continue
		}
		val := vec.Get(i)
		if val == 0 {
			continue
		}
		rName := v1.ResourceName(name)
		if rName == v1.ResourceEphemeralStorage || rName == v1.ResourceStorage {
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

func (v ResourceVector) ToResourceQuantities(indexMap *ResourceVectorMap) map[string]float64 {
	result := make(map[string]float64, indexMap.Len())
	for i := 0; i < indexMap.Len(); i++ {
		name := indexMap.ResourceAt(i)
		result[name] = v.Get(i)
	}
	return result
}
