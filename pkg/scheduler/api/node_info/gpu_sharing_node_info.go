// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"math"

	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type GpuSharingNodeInfo struct {
	// All
	ReleasingSharedGPUs map[string]bool

	// Fractional
	UsedSharedGPUsMemory      map[string]int64
	ReleasingSharedGPUsMemory map[string]int64
	AllocatedSharedGPUsMemory map[string]int64
}

func newGpuSharingNodeInfo() *GpuSharingNodeInfo {
	return &GpuSharingNodeInfo{
		ReleasingSharedGPUs: make(map[string]bool),

		UsedSharedGPUsMemory:      make(map[string]int64),
		ReleasingSharedGPUsMemory: make(map[string]int64),
		AllocatedSharedGPUsMemory: make(map[string]int64),
	}
}

func (g *GpuSharingNodeInfo) Clone() *GpuSharingNodeInfo {
	gpuSharingNodeInfo := newGpuSharingNodeInfo()

	for k, v := range g.ReleasingSharedGPUs {
		gpuSharingNodeInfo.ReleasingSharedGPUs[k] = v
	}
	for k, v := range g.UsedSharedGPUsMemory {
		gpuSharingNodeInfo.UsedSharedGPUsMemory[k] = v
	}
	for k, v := range g.ReleasingSharedGPUsMemory {
		gpuSharingNodeInfo.ReleasingSharedGPUsMemory[k] = v
	}
	for k, v := range g.AllocatedSharedGPUsMemory {
		gpuSharingNodeInfo.AllocatedSharedGPUsMemory[k] = v
	}

	return gpuSharingNodeInfo
}

/************* All - Shared Tasks *************/

func getAcceptedTaskResourceWithoutSharedGPU(task *pod_info.PodInfo) *resource_info.Resource {
	requestedResourceWithoutSharedGPU := resource_info.EmptyResource()
	requestedResourceWithoutSharedGPU.BaseResource = *task.AcceptedResource.BaseResource.Clone()
	requestedResourceWithoutSharedGPU.SetGPUs(task.AcceptedResource.GPUs())
	requestedResourceWithoutSharedGPU.AddGPUs(float64(task.AcceptedResource.GetDraGpusCount()))
	maps.Copy(requestedResourceWithoutSharedGPU.ScalarResources(), task.AcceptedResource.MigResources())
	maps.Copy(requestedResourceWithoutSharedGPU.ScalarResources(), task.AcceptedResource.ScalarResources())
	if task.IsSharedGPUAllocation() {
		requestedResourceWithoutSharedGPU.SetGPUs(0)
	}

	return requestedResourceWithoutSharedGPU
}

func (ni *NodeInfo) addSharedGPUTaskResources(task *pod_info.PodInfo) {
	if !task.IsSharedGPUAllocation() {
		return
	}

	log.InfraLogger.V(7).Infof("About to add shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
		task.Namespace, task.Name, task.Status, ni)

	for _, gpuGroup := range task.GPUGroups {
		ni.addSharedGPUTaskResourcesPerPodGroup(task, gpuGroup)
	}

	log.InfraLogger.V(8).Infof("Added shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
		task.Namespace, task.Name, task.Status, ni)
}

func (ni *NodeInfo) addSharedGPUTaskResourcesPerPodGroup(task *pod_info.PodInfo, gpuGroup string) {
	log.InfraLogger.V(7).Infof(
		"About to add shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
			"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
		task.Namespace, task.Name, task.GPUGroups,
		ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
		ni.UsedSharedGPUsMemory[gpuGroup])

	ni.UsedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(task.ResReq)
	singleGpu := resource_info.NewSingleGpuVector(ni.VectorMap)

	switch task.Status {
	case pod_status.Releasing:
		ni.ReleasingSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(task.ResReq)
		ni.AllocatedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(task.ResReq)

		if ni.UsedSharedGPUsMemory[gpuGroup] == ni.ReleasingSharedGPUsMemory[gpuGroup] {
			// is this the last releasing task for this gpu
			if !ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
				ni.Releasing.AddGPUs(1)
				ni.ReleasingVector.Add(singleGpu)
				ni.markSharedGpuAsReleasing(gpuGroup)
			}
			if int(ni.GetNumberOfGPUsInNode()) < int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.Idle.SubGPUs(1)
				ni.IdleVector.Sub(singleGpu)
			}
		}
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(task.ResReq)

		if ni.UsedSharedGPUsMemory[gpuGroup]-ni.GetResourceGpuMemory(task.ResReq) ==
			ni.ReleasingSharedGPUsMemory[gpuGroup]+ni.GetResourceGpuMemory(task.ResReq) {
			ni.Releasing.SubGPUs(1)
			ni.ReleasingVector.Sub(singleGpu)
		}
	default:
		ni.AllocatedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(task.ResReq)

		if ni.UsedSharedGPUsMemory[gpuGroup] <= ni.GetResourceGpuMemory(task.ResReq) {
			// no other fractional was allocated here yet
			if int(ni.GetNumberOfGPUsInNode()) < int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.Idle.SubGPUs(1)
				ni.IdleVector.Sub(singleGpu)
			}
		}

		if ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
			ni.Releasing.SubGPUs(1)
			ni.ReleasingVector.Sub(singleGpu)
			ni.unmarkSharedGpuAsReleasing(gpuGroup)
		}
	}

	log.InfraLogger.V(8).Infof(
		"Added shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
			"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
		task.Namespace, task.Name, task.GPUGroups,
		ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
		ni.UsedSharedGPUsMemory[gpuGroup])
}

func (ni *NodeInfo) removeSharedTaskResources(task *pod_info.PodInfo) {
	if !task.IsSharedGPUAllocation() {
		return
	}

	log.InfraLogger.V(7).Infof(
		"About to remove shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
		task.Namespace, task.Name, task.GPUGroups, ni)

	for _, gpuGroup := range task.GPUGroups {
		ni.removeSharedTaskResourcesPerPodGroup(task, gpuGroup)
	}

	log.InfraLogger.V(8).Infof(
		"Removed shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
		task.Namespace, task.Name, task.Status, ni)
}

func (ni *NodeInfo) removeSharedTaskResourcesPerPodGroup(task *pod_info.PodInfo, gpuGroup string) {
	log.InfraLogger.V(7).Infof(
		"About to remove shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
			"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
		task.Namespace, task.Name, task.GPUGroups,
		ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
		ni.UsedSharedGPUsMemory[gpuGroup])

	ni.UsedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(task.ResReq)
	singleGpu := resource_info.NewSingleGpuVector(ni.VectorMap)

	switch task.Status {
	case pod_status.Releasing:
		ni.ReleasingSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(task.ResReq)
		ni.AllocatedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(task.ResReq)
		log.InfraLogger.V(6).Infof(
			"Releasing gpuGroup: <%v> releasingSharedGPU: <%v> "+
				"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
			ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])

		if ni.UsedSharedGPUsMemory[gpuGroup] <= 0 {
			// is this the last releasing task for this gpu
			if int(ni.GetNumberOfGPUsInNode()) >= int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.Idle.AddGPUs(1)
				ni.IdleVector.Add(singleGpu)
			}
			if ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
				ni.Releasing.SubGPUs(1)
				ni.ReleasingVector.Sub(singleGpu)
				ni.unmarkSharedGpuAsReleasing(gpuGroup)
			}
		}
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(task.ResReq)
		log.InfraLogger.V(6).Infof(
			"Pipelined gpuGroup: <%v> releasingSharedGPU: <%v> "+
				"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
			ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])

		if ni.isPipelinedToReleasingGpu(task, gpuGroup) {
			// no other fractional was pipelined here yet
			ni.Releasing.AddGPUs(1)
			ni.ReleasingVector.Add(singleGpu)
		}
	default:
		log.InfraLogger.V(6).Infof(
			"other gpuGroup: <%v> releasingSharedGPU: <%v> "+
				"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
			ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])
		ni.AllocatedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(task.ResReq)

		if ni.UsedSharedGPUsMemory[gpuGroup] <= 0 {
			// no other fractional was allocated here yet
			if int(ni.GetNumberOfGPUsInNode()) >= int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.Idle.AddGPUs(1)
				ni.IdleVector.Add(singleGpu)
			}
		}

		if ni.isGpuReleasingFromSharedTasks(gpuGroup) && !ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
			ni.Releasing.AddGPUs(1)
			ni.ReleasingVector.Add(singleGpu)
			ni.markSharedGpuAsReleasing(gpuGroup)
		}
	}

	log.InfraLogger.V(8).Infof(
		"Removed shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
			"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
		task.Namespace, task.Name, task.GPUGroups,
		ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
		ni.UsedSharedGPUsMemory[gpuGroup])
}

func (ni *NodeInfo) isPipelinedToReleasingGpu(task *pod_info.PodInfo, gpuGroup string) bool {
	usedMemoryBeforeRemoval := ni.UsedSharedGPUsMemory[gpuGroup] + ni.GetResourceGpuMemory(task.ResReq)
	releasingMemoryBeforeRemoval := ni.ReleasingSharedGPUsMemory[gpuGroup] - ni.GetResourceGpuMemory(task.ResReq)
	usedOriginally0 := ni.UsedSharedGPUsMemory[gpuGroup] == 0
	releasingOriginally0 := ni.ReleasingSharedGPUsMemory[gpuGroup] == 0

	return (usedMemoryBeforeRemoval == releasingMemoryBeforeRemoval) || (usedOriginally0 && releasingOriginally0)
}

func (ni *NodeInfo) ConsolidateSharedPodInfoToDifferentGPU(ti *pod_info.PodInfo) error {
	return ni.addTask(ti, true)
}

func (ni *NodeInfo) isGpuReleasingFromSharedTasks(gpuGroup string) bool {
	usedSharedGPUsMemory, found := ni.UsedSharedGPUsMemory[gpuGroup]
	if !found || usedSharedGPUsMemory == 0 {
		// the GPU is free - not Releasing
		return false
	}

	releasingSharedGPUsMemory, found := ni.ReleasingSharedGPUsMemory[gpuGroup]
	if found && releasingSharedGPUsMemory == usedSharedGPUsMemory {
		// all used fractional memory on this GPU is actually releasing
		return true
	}

	return false
}

func (ni *NodeInfo) getNumberOfUsedSharedGPUs() int {
	numberOfSharedGPUs := 0
	for _, sharedGPUs := range ni.UsedSharedGPUsMemory {
		if sharedGPUs > 0 {
			numberOfSharedGPUs++
		}
	}

	return numberOfSharedGPUs
}

func (ni *NodeInfo) getNumberOfUsedGPUs() int {
	return int(ni.UsedVector.Get(resource_info.GPUIndex)) + ni.getNumberOfUsedSharedGPUs()
}

func (ni *NodeInfo) GetNumberOfAllocatedSharedGPUs() int {
	numberOfAllocatedSharedGPUs := 0
	for _, sharedGPUs := range ni.AllocatedSharedGPUsMemory {
		if sharedGPUs > 0 {
			numberOfAllocatedSharedGPUs++
		}
	}

	return numberOfAllocatedSharedGPUs
}

func (ni *NodeInfo) isSharedGpuMarkedAsReleasing(gpuGroup string) bool {
	isReleasing, found := ni.ReleasingSharedGPUs[gpuGroup]
	return found && isReleasing
}

// AvailableSharedGPUFractions returns the total available GPU capacity from partially-used
// shared GPU devices, expressed as a fraction of whole GPUs. Each device's remaining memory
// is converted to a fraction of the device's total memory.
func (ni *NodeInfo) AvailableSharedGPUFractions() float64 {
	if ni.MemoryOfEveryGpuOnNode <= 0 {
		return 0
	}
	total := 0.0
	for gpuGroup, usedMemory := range ni.UsedSharedGPUsMemory {
		if usedMemory <= 0 {
			continue
		}
		allocated := ni.AllocatedSharedGPUsMemory[gpuGroup]
		releasing := ni.ReleasingSharedGPUsMemory[gpuGroup]
		availableMemory := ni.MemoryOfEveryGpuOnNode - allocated + releasing
		if availableMemory > 0 {
			total += float64(availableMemory) / float64(ni.MemoryOfEveryGpuOnNode)
		}
	}
	return total
}

func (ni *NodeInfo) markSharedGpuAsReleasing(gpuGroup string) {
	ni.ReleasingSharedGPUs[gpuGroup] = true
}

func (ni *NodeInfo) unmarkSharedGpuAsReleasing(gpuGroup string) {
	delete(ni.ReleasingSharedGPUs, gpuGroup)
}

/************* Fractions *************/

func (ni *NodeInfo) getSumOfAvailableSharedGPUs() (float64, int64) {
	sumOfSharedGPUs := float64(0)
	sumOfSharedGPUsMemory := int64(0)
	for _, allocatedSharedGPUs := range ni.AllocatedSharedGPUsMemory {
		if allocatedSharedGPUs > 0 {
			sumOfSharedGPUs += 1 - ni.getGpuMemoryFractionalOnNode(allocatedSharedGPUs)
			sumOfSharedGPUsMemory += ni.MemoryOfEveryGpuOnNode - allocatedSharedGPUs
		}
	}
	return sumOfSharedGPUs, sumOfSharedGPUsMemory
}

func (ni *NodeInfo) getSumOfReleasingSharedGPUs() (float64, int64) {
	sumOfSharedGPUs := float64(0)
	sumOfSharedGPUsMemory := int64(0)
	for gpuGroup, releasingSharedGPUs := range ni.ReleasingSharedGPUsMemory {
		if releasingSharedGPUs > 0 && !ni.isGpuReleasingFromSharedTasks(gpuGroup) {
			sumOfSharedGPUs += ni.getGpuMemoryFractionalOnNode(releasingSharedGPUs)
			sumOfSharedGPUsMemory += releasingSharedGPUs
		}
	}
	return sumOfSharedGPUs, sumOfSharedGPUsMemory
}

func (ni *NodeInfo) getGpuMemoryFractionalOnNode(memory int64) float64 {
	exactFraction := float64(memory) / float64(ni.MemoryOfEveryGpuOnNode)
	return math.Ceil(exactFraction*100) / 100
}

func (ni *NodeInfo) fractionTaskGpusAllocatableDeviceCount(pod *pod_info.PodInfo) int64 {
	matchingGpuGroupsCount := int64(0)
	for gpuGroup := range ni.UsedSharedGPUsMemory {
		if ni.IsTaskFitOnGpuGroup(pod.ResReq, gpuGroup) {
			matchingGpuGroupsCount += 1
			if matchingGpuGroupsCount >= pod.ResReq.GetNumOfGpuDevices() {
				return matchingGpuGroupsCount
			}
		}
	}

	return matchingGpuGroupsCount
}

func (ni *NodeInfo) IsTaskFitOnGpuGroup(resourceRequest *resource_info.ResourceRequirements, gpuGroup string) bool {
	return ni.UsedSharedGPUsMemory[gpuGroup] != 0 &&
		ni.enoughResourcesOnGpu(resourceRequest, gpuGroup) &&
		!ni.isAllGpuReleased(gpuGroup)
}

func (ni *NodeInfo) EnoughIdleResourcesOnGpu(resources *resource_info.ResourceRequirements, gpuGroup string) bool {
	if _, foundOnAllocated := ni.AllocatedSharedGPUsMemory[gpuGroup]; !foundOnAllocated {
		// If a gpu group is not found in allocated, it's an indication that this group is pipelined
		return false
	}
	return ni.MemoryOfEveryGpuOnNode-ni.AllocatedSharedGPUsMemory[gpuGroup]-ni.GetResourceGpuMemory(resources) >= 0
}

func (ni *NodeInfo) enoughResourcesOnGpu(resources *resource_info.ResourceRequirements, gpuGroup string) bool {
	return (ni.MemoryOfEveryGpuOnNode -
		ni.AllocatedSharedGPUsMemory[gpuGroup] +
		ni.ReleasingSharedGPUsMemory[gpuGroup] -
		ni.GetResourceGpuMemory(resources)) >= 0
}

func (ni *NodeInfo) isAllGpuReleased(gpuGroup string) bool {
	return ni.AllocatedSharedGPUsMemory[gpuGroup] == ni.ReleasingSharedGPUsMemory[gpuGroup]
}

func (ni *NodeInfo) GetUsedGpuPortion(gpuIdx string) (float64, error) {
	if ni.MemoryOfEveryGpuOnNode < DefaultGpuMemory || ni.MemoryOfEveryGpuOnNode == 0 {
		return 0, fmt.Errorf("node <%s> has invalid GPU memory", ni.Name)
	}

	return float64(ni.UsedSharedGPUsMemory[gpuIdx]) / float64(ni.MemoryOfEveryGpuOnNode), nil
}
