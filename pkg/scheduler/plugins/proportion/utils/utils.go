// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

func QuantifyResource(resource *resource_info.Resource) rs.ResourceQuantities {
	return rs.NewResourceQuantities(resource.Cpu(), resource.Memory(), resource.GetTotalGPURequest())
}

func QuantifyResourceRequirements(resource *resource_info.ResourceRequirements) rs.ResourceQuantities {
	return rs.NewResourceQuantities(resource.Cpu(), resource.Memory(), resource.GetGpusQuota())
}

func QuantifyVector(vec resource_info.ResourceVector, vectorMap *resource_info.ResourceVectorMap) rs.ResourceQuantities {
	totalGPUs := vec.Get(resource_info.GPUIndex)
	for i := range vectorMap.Len() {
		name := vectorMap.ResourceAt(i)
		if !resource_info.IsMigResource(v1.ResourceName(name)) {
			continue
		}
		gpuPortion, _, err := resources.ExtractGpuAndMemoryFromMigResourceName(name)
		if err != nil {
			log.InfraLogger.Errorf("Failed to get device portion from %v", name)
			continue
		}
		totalGPUs += float64(gpuPortion) * vec.Get(i)
	}
	return rs.NewResourceQuantities(vec.Get(resource_info.CPUIndex), vec.Get(resource_info.MemoryIndex), totalGPUs)
}

func ResourceRequirementsFromQuantities(quantities rs.ResourceQuantities) *resource_info.ResourceRequirements {
	return resource_info.NewResourceRequirements(
		quantities[rs.GpuResource],
		quantities[rs.CpuResource],
		quantities[rs.MemoryResource],
	)
}
