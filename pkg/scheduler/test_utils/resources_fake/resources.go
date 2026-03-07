// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources_fake

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

const (
	DefaultCPU      = "1"
	DefaultMemory   = "1G"
	DefaultGPUMilli = "2"
)

func BuildResource(cpuMilliInput *string, memoryInput *string, gpuInput *string,
	migInstances map[v1.ResourceName]int) *resource_info.Resource {
	migInstanceCount := MigInstancesToMigInstanceCount(migInstances)

	resourceList := BuildResourceList(cpuMilliInput, memoryInput, gpuInput, migInstanceCount)
	return resource_info.ResourceFromResourceList(*resourceList)
}

func BuildResourceRequirements(cpuMilliInput *string, memoryInput *string, gpuInput *string,
	migInstances map[v1.ResourceName]int) *resource_info.ResourceRequirements {
	migInstanceCount := MigInstancesToMigInstanceCount(migInstances)

	resourceList := BuildResourceList(cpuMilliInput, memoryInput, gpuInput, migInstanceCount)
	return resource_info.RequirementsFromResourceList(*resourceList)
}

func BuildResourceList(cpuMilliInput *string, memoryInput *string, gpuInput *string,
	migInstances map[v1.ResourceName]int) *v1.ResourceList {
	if cpuMilliInput == nil && memoryInput == nil && gpuInput == nil {
		return &v1.ResourceList{}
	}

	var cpu, memory, gpu string

	if cpuMilliInput != nil {
		cpu = *cpuMilliInput
	} else {
		cpu = DefaultCPU
	}

	if memoryInput != nil {
		memory = *memoryInput
	} else {
		memory = DefaultMemory
	}

	if gpuInput != nil {
		gpu = *gpuInput
	} else {
		gpu = DefaultGPUMilli
	}

	if gpu == "" {
		return &v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(cpu),
			v1.ResourceMemory: resource.MustParse(memory),
		}
	}

	resources := v1.ResourceList{
		v1.ResourceCPU:                resource.MustParse(cpu),
		v1.ResourceMemory:             resource.MustParse(memory),
		resource_info.GPUResourceName: resource.MustParse(gpu),
	}

	for migInstance, count := range migInstances {
		resources[migInstance] = resource.MustParse(fmt.Sprint(count))
	}

	return &resources
}

func MigInstancesToMigInstanceCount(migInstances map[v1.ResourceName]int,
) map[v1.ResourceName]int {
	migInstanceCount := map[v1.ResourceName]int{}
	for migInstance, count := range migInstances {
		migInstanceCount[migInstance] = count
	}

	return migInstanceCount
}
