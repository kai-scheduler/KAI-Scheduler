// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	commonconstants "github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	"github.com/kai-scheduler/api/utilities/resources"
)

type GpuSharingNodeInfo struct {
	// All
	ReleasingSharedGPUs map[string]bool

	// Fractional
	UsedSharedGPUsMemory      map[string]int64
	ReleasingSharedGPUsMemory map[string]int64
	AllocatedSharedGPUsMemory map[string]int64

	// Compute, in resource_info.GpuComputeScale units, mirroring the memory maps above.
	// ReleasingSharedGPUsCompute is net of pipelined reservations and may be negative.
	UsedSharedGPUsCompute      map[string]int64
	AllocatedSharedGPUsCompute map[string]int64
	ReleasingSharedGPUsCompute map[string]int64

	// DRASharedDeviceRefCount counts how many pods on the node reference each
	// physical DRA GPU device (keyed by driver/pool/device). A device shared
	// by several pods through one ResourceClaim (status.reservedFor with more
	// than one entry) must contribute to the node's used GPU count only once.
	DRASharedDeviceRefCount map[string]int
}

func newGpuSharingNodeInfo() *GpuSharingNodeInfo {
	return &GpuSharingNodeInfo{
		ReleasingSharedGPUs: make(map[string]bool),

		UsedSharedGPUsMemory:      make(map[string]int64),
		ReleasingSharedGPUsMemory: make(map[string]int64),
		AllocatedSharedGPUsMemory: make(map[string]int64),

		UsedSharedGPUsCompute:      make(map[string]int64),
		AllocatedSharedGPUsCompute: make(map[string]int64),
		ReleasingSharedGPUsCompute: make(map[string]int64),

		DRASharedDeviceRefCount: make(map[string]int),
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
	for k, v := range g.UsedSharedGPUsCompute {
		gpuSharingNodeInfo.UsedSharedGPUsCompute[k] = v
	}
	for k, v := range g.AllocatedSharedGPUsCompute {
		gpuSharingNodeInfo.AllocatedSharedGPUsCompute[k] = v
	}
	for k, v := range g.ReleasingSharedGPUsCompute {
		gpuSharingNodeInfo.ReleasingSharedGPUsCompute[k] = v
	}
	for k, v := range g.DRASharedDeviceRefCount {
		gpuSharingNodeInfo.DRASharedDeviceRefCount[k] = v
	}

	return gpuSharingNodeInfo
}

func (ni *NodeInfo) IsGpuGroupComputeSharingModeCompatible(gpuGroup string, task *pod_info.PodInfo) bool {
	requestedMode := task.RequestedGPUComputeSharingMode()
	if mode, found := ni.getReservationPodGpuGroupComputeSharingMode(gpuGroup); found {
		return mode == requestedMode
	}
	if mode, found := ni.getGpuGroupComputeSharingMode(gpuGroup); found {
		return mode == requestedMode
	}
	for _, fractionalGpuGroup := range task.FractionalGpuGroupsOrDefault() {
		fractionalGpuGroup = fractionalGpuGroup.WithDefaults()
		if fractionalGpuGroup.ID == gpuGroup {
			return fractionalGpuGroup.ComputeSharingMode == requestedMode
		}
	}
	return requestedMode == schedulingv1alpha2.GPUComputeSharingModeTimeSlicing
}

func (ni *NodeInfo) getReservationPodGpuGroupComputeSharingMode(
	gpuGroup string,
) (schedulingv1alpha2.GPUComputeSharingMode, bool) {
	for _, task := range ni.PodInfos {
		if task.Pod == nil ||
			!strings.HasPrefix(task.Pod.Name, commonconstants.GPUReservationPodPrefix) ||
			task.Pod.Labels[commonconstants.GPUGroup] != gpuGroup {
			continue
		}
		if task.Pod.Annotations == nil {
			return schedulingv1alpha2.GPUComputeSharingModeTimeSlicing, true
		}
		_, rawMode, _ := resources.ExtractGpuComputeSharingModeAnnotation(task.Pod)
		return schedulingv1alpha2.DefaultGPUComputeSharingMode(schedulingv1alpha2.GPUComputeSharingMode(rawMode)), true
	}
	return "", false
}

func (ni *NodeInfo) getGpuGroupComputeSharingMode(gpuGroup string) (schedulingv1alpha2.GPUComputeSharingMode, bool) {
	for _, task := range ni.PodInfos {
		for _, fractionalGpuGroup := range task.FractionalGpuGroupsOrDefault() {
			if fractionalGpuGroup.ID == gpuGroup {
				return fractionalGpuGroup.WithDefaults().ComputeSharingMode, true
			}
		}
	}
	return "", false
}

/************* All - Shared Tasks *************/

func getAcceptedTaskResourceVectorWithoutSharedGPU(task *pod_info.PodInfo, vectorMap *resource_info.ResourceVectorMap) resource_info.ResourceVector {
	vec := task.AcceptedResourceVector.Clone()
	if task.IsSharedGPUAllocation() {
		vec.Set(resource_info.GPUIndex, 0)
	}
	return vec
}

func (ni *NodeInfo) addSharedGPUTaskResources(task *pod_info.PodInfo) {
	if !task.IsSharedGPUAllocation() {
		return
	}

	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof("About to add shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
			task.Namespace, task.Name, task.Status, ni)
	})

	for _, gpuGroup := range task.GPUGroupIDs() {
		ni.addSharedGPUTaskResourcesPerPodGroup(task, gpuGroup)
	}

	log.InfraLogger.V(8).Do(func() {
		log.InfraLogger.Infof("Added shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
			task.Namespace, task.Name, task.Status, ni)
	})
}

func (ni *NodeInfo) addSharedGPUTaskResourcesPerPodGroup(task *pod_info.PodInfo, gpuGroup string) {
	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof(
			"About to add shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
				"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			task.Namespace, task.Name, task.GPUGroupIDs(),
			ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
			ni.UsedSharedGPUsMemory[gpuGroup])
	})

	ni.UsedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(&task.GpuRequirement)
	ni.addSharedGPUTaskCompute(task, gpuGroup)
	singleGpu := resource_info.NewSingleGpuVector(ni.VectorMap)

	switch task.Status {
	case pod_status.Releasing:
		ni.ReleasingSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(&task.GpuRequirement)
		ni.AllocatedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(&task.GpuRequirement)

		if ni.UsedSharedGPUsMemory[gpuGroup] == ni.ReleasingSharedGPUsMemory[gpuGroup] {
			// is this the last releasing task for this gpu
			if !ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
				ni.ReleasingVector.Add(singleGpu)
				ni.markSharedGpuAsReleasing(gpuGroup)
			}
			if int(ni.GetNumberOfGPUsInNode()) < int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.IdleVector.Sub(singleGpu)
			}
		}
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(&task.GpuRequirement)

		if ni.UsedSharedGPUsMemory[gpuGroup]-ni.GetResourceGpuMemory(&task.GpuRequirement) ==
			ni.ReleasingSharedGPUsMemory[gpuGroup]+ni.GetResourceGpuMemory(&task.GpuRequirement) {
			ni.ReleasingVector.Sub(singleGpu)
		}
	default:
		ni.AllocatedSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(&task.GpuRequirement)

		if ni.UsedSharedGPUsMemory[gpuGroup] <= ni.GetResourceGpuMemory(&task.GpuRequirement) {
			// no other fractional was allocated here yet
			if int(ni.GetNumberOfGPUsInNode()) < int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.IdleVector.Sub(singleGpu)
			}
		}

		if ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
			ni.ReleasingVector.Sub(singleGpu)
			ni.unmarkSharedGpuAsReleasing(gpuGroup)
		}
	}

	log.InfraLogger.V(8).Do(func() {
		log.InfraLogger.Infof(
			"Added shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
				"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			task.Namespace, task.Name, task.GPUGroupIDs(),
			ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
			ni.UsedSharedGPUsMemory[gpuGroup])
	})
}

func (ni *NodeInfo) removeSharedTaskResources(task *pod_info.PodInfo) {
	if !task.IsSharedGPUAllocation() {
		return
	}

	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof(
			"About to remove shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
			task.Namespace, task.Name, task.GPUGroupIDs(), ni)
	})

	for _, gpuGroup := range task.GPUGroupIDs() {
		ni.removeSharedTaskResourcesPerPodGroup(task, gpuGroup)
	}

	log.InfraLogger.V(8).Do(func() {
		log.InfraLogger.Infof(
			"Removed shared podsInfo: <%v/%v>, status: <%v>, node: <%+v>",
			task.Namespace, task.Name, task.Status, ni)
	})
}

func (ni *NodeInfo) removeSharedTaskResourcesPerPodGroup(task *pod_info.PodInfo, gpuGroup string) {
	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof(
			"About to remove shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
				"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			task.Namespace, task.Name, task.GPUGroupIDs(),
			ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
			ni.UsedSharedGPUsMemory[gpuGroup])
	})

	ni.UsedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(&task.GpuRequirement)
	ni.removeSharedGPUTaskCompute(task, gpuGroup)
	singleGpu := resource_info.NewSingleGpuVector(ni.VectorMap)

	switch task.Status {
	case pod_status.Releasing:
		ni.ReleasingSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(&task.GpuRequirement)
		ni.AllocatedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(&task.GpuRequirement)
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof(
				"Releasing gpuGroup: <%v> releasingSharedGPU: <%v> "+
					"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
				gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
				ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])
		})

		if ni.UsedSharedGPUsMemory[gpuGroup] <= 0 {
			// is this the last releasing task for this gpu
			if int(ni.GetNumberOfGPUsInNode()) >= int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.IdleVector.Add(singleGpu)
			}
			if ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
				ni.ReleasingVector.Sub(singleGpu)
				ni.unmarkSharedGpuAsReleasing(gpuGroup)
			}
		}
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsMemory[gpuGroup] += ni.GetResourceGpuMemory(&task.GpuRequirement)
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof(
				"Pipelined gpuGroup: <%v> releasingSharedGPU: <%v> "+
					"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
				gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
				ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])
		})

		if ni.isPipelinedToReleasingGpu(task, gpuGroup) {
			// no other fractional was pipelined here yet
			ni.ReleasingVector.Add(singleGpu)
		}
	default:
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof(
				"other gpuGroup: <%v> releasingSharedGPU: <%v> "+
					"AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
				gpuGroup, ni.ReleasingSharedGPUsMemory[gpuGroup],
				ni.AllocatedSharedGPUsMemory[gpuGroup], ni.UsedSharedGPUsMemory[gpuGroup])
		})
		ni.AllocatedSharedGPUsMemory[gpuGroup] -= ni.GetResourceGpuMemory(&task.GpuRequirement)

		if ni.UsedSharedGPUsMemory[gpuGroup] <= 0 {
			// no other fractional was allocated here yet
			if int(ni.GetNumberOfGPUsInNode()) >= int(ni.IdleVector.Get(resource_info.GPUIndex))+ni.getNumberOfUsedGPUs() {
				ni.IdleVector.Add(singleGpu)
			}
		}

		if ni.isGpuReleasingFromSharedTasks(gpuGroup) && !ni.isSharedGpuMarkedAsReleasing(gpuGroup) {
			ni.ReleasingVector.Add(singleGpu)
			ni.markSharedGpuAsReleasing(gpuGroup)
		}
	}

	log.InfraLogger.V(8).Do(func() {
		log.InfraLogger.Infof(
			"Removed shared podsInfo: <%v/%v>, gpuGroup: <%v> "+
				"releasingSharedGPU: <%v> AllocatedSharedGPUsMemory <%v>, UsedSharedGPUsMemory: <%v>",
			task.Namespace, task.Name, task.GPUGroupIDs(),
			ni.ReleasingSharedGPUsMemory[gpuGroup], ni.AllocatedSharedGPUsMemory[gpuGroup],
			ni.UsedSharedGPUsMemory[gpuGroup])
	})
}

func (ni *NodeInfo) isPipelinedToReleasingGpu(task *pod_info.PodInfo, gpuGroup string) bool {
	usedMemoryBeforeRemoval := ni.UsedSharedGPUsMemory[gpuGroup] + ni.GetResourceGpuMemory(&task.GpuRequirement)
	releasingMemoryBeforeRemoval := ni.ReleasingSharedGPUsMemory[gpuGroup] - ni.GetResourceGpuMemory(&task.GpuRequirement)
	usedOriginally0 := ni.UsedSharedGPUsMemory[gpuGroup] == 0
	releasingOriginally0 := ni.ReleasingSharedGPUsMemory[gpuGroup] == 0

	return (usedMemoryBeforeRemoval == releasingMemoryBeforeRemoval) || (usedOriginally0 && releasingOriginally0)
}

func (ni *NodeInfo) ConsolidateSharedPodInfoToDifferentGPU(ti *pod_info.PodInfo) error {
	return ni.addTask(ti, true)
}

// addSharedGPUTaskCompute mirrors the memory ledger branches of addSharedGPUTaskResourcesPerPodGroup.
// Compute never drives GPU occupancy vectors: a zero-compute (memory-only) pod still occupies the GPU.
func (ni *NodeInfo) addSharedGPUTaskCompute(task *pod_info.PodInfo, gpuGroup string) {
	compute := task.GpuRequirement.GpuCompute()
	if compute == 0 {
		return
	}
	ni.ensureSharedGPUComputeMaps()

	ni.UsedSharedGPUsCompute[gpuGroup] += compute
	switch task.Status {
	case pod_status.Releasing:
		ni.AllocatedSharedGPUsCompute[gpuGroup] += compute
		ni.ReleasingSharedGPUsCompute[gpuGroup] += compute
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsCompute[gpuGroup] -= compute
	default:
		ni.AllocatedSharedGPUsCompute[gpuGroup] += compute
	}
}

func (ni *NodeInfo) removeSharedGPUTaskCompute(task *pod_info.PodInfo, gpuGroup string) {
	compute := task.GpuRequirement.GpuCompute()
	if compute == 0 {
		return
	}
	ni.ensureSharedGPUComputeMaps()

	ni.UsedSharedGPUsCompute[gpuGroup] -= compute
	switch task.Status {
	case pod_status.Releasing:
		ni.AllocatedSharedGPUsCompute[gpuGroup] -= compute
		ni.ReleasingSharedGPUsCompute[gpuGroup] -= compute
	case pod_status.Pipelined:
		ni.ReleasingSharedGPUsCompute[gpuGroup] += compute
	default:
		ni.AllocatedSharedGPUsCompute[gpuGroup] -= compute
	}
}

func (ni *NodeInfo) ensureSharedGPUComputeMaps() {
	if ni.UsedSharedGPUsCompute == nil {
		ni.UsedSharedGPUsCompute = make(map[string]int64)
	}
	if ni.AllocatedSharedGPUsCompute == nil {
		ni.AllocatedSharedGPUsCompute = make(map[string]int64)
	}
	if ni.ReleasingSharedGPUsCompute == nil {
		ni.ReleasingSharedGPUsCompute = make(map[string]int64)
	}
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
		if ni.IsTaskFitOnGpuGroup(pod, gpuGroup) {
			matchingGpuGroupsCount += 1
			if matchingGpuGroupsCount >= pod.GpuRequirement.GetNumOfGpuDevices() {
				return matchingGpuGroupsCount
			}
		}
	}

	return matchingGpuGroupsCount
}

func (ni *NodeInfo) IsTaskFitOnGpuGroup(task *pod_info.PodInfo, gpuGroup string) bool {
	return ni.isTaskFitOnGpuGroupWithoutCompute(task, gpuGroup) &&
		ni.enoughComputeOnGpu(&task.GpuRequirement, gpuGroup)
}

func (ni *NodeInfo) isTaskFitOnGpuGroupWithoutCompute(task *pod_info.PodInfo, gpuGroup string) bool {
	return ni.UsedSharedGPUsMemory[gpuGroup] != 0 &&
		ni.enoughResourcesOnGpu(&task.GpuRequirement, gpuGroup) &&
		!ni.isAllGpuReleased(gpuGroup) &&
		ni.IsGpuGroupComputeSharingModeCompatible(gpuGroup, task)
}

func (ni *NodeInfo) EnoughIdleResourcesOnGpu(resources *resource_info.GpuResourceRequirement, gpuGroup string) bool {
	if _, foundOnAllocated := ni.AllocatedSharedGPUsMemory[gpuGroup]; !foundOnAllocated {
		// If a gpu group is not found in allocated, it's an indication that this group is pipelined
		return false
	}
	if ni.MemoryOfEveryGpuOnNode-ni.AllocatedSharedGPUsMemory[gpuGroup]-ni.GetResourceGpuMemory(resources) < 0 {
		return false
	}
	compute := resources.GpuCompute()
	return compute == 0 || resource_info.GpuComputeScale-ni.AllocatedSharedGPUsCompute[gpuGroup]-compute >= 0
}

// AvailableSharedGpuCompute returns the compute a new placement may claim on the GPU group,
// counting capacity that is still releasing.
func (ni *NodeInfo) AvailableSharedGpuCompute(gpuGroup string) int64 {
	return resource_info.GpuComputeScale -
		ni.AllocatedSharedGPUsCompute[gpuGroup] +
		ni.ReleasingSharedGPUsCompute[gpuGroup]
}

func (ni *NodeInfo) enoughComputeOnGpu(resources *resource_info.GpuResourceRequirement, gpuGroup string) bool {
	compute := resources.GpuCompute()
	return compute == 0 || ni.AvailableSharedGpuCompute(gpuGroup)-compute >= 0
}

// GpuComputeFitError reports a node that has enough GPU memory for the task but whose eligible GPUs
// lack the requested compute. It returns nil when compute is not the bottleneck.
func (ni *NodeInfo) GpuComputeFitError(task *pod_info.PodInfo) *common_info.TasksFitError {
	compute := task.GpuRequirement.GpuCompute()
	if compute <= 0 || !task.IsSharedGPURequest() {
		return nil
	}
	requiredDevices := int(task.GpuRequirement.GetNumOfGpuDevices())
	if requiredDevices <= 0 {
		return nil
	}

	var eligibleFreeCompute []int64
	fittingDevices := 0
	for gpuGroup := range ni.UsedSharedGPUsMemory {
		if !ni.isTaskFitOnGpuGroupWithoutCompute(task, gpuGroup) {
			continue
		}
		eligibleFreeCompute = append(eligibleFreeCompute, ni.AvailableSharedGpuCompute(gpuGroup))
		if ni.enoughComputeOnGpu(&task.GpuRequirement, gpuGroup) {
			fittingDevices++
		}
	}
	wholeGpus := int(math.Floor(ni.nonAllocatedVector().Get(resource_info.GPUIndex)))
	for range wholeGpus {
		eligibleFreeCompute = append(eligibleFreeCompute, resource_info.GpuComputeScale)
		if compute <= resource_info.GpuComputeScale {
			fittingDevices++
		}
	}

	if len(eligibleFreeCompute) < requiredDevices || fittingDevices >= requiredDevices {
		return nil
	}

	// The kth-largest free compute is what limits a k-device request; compute never sums across GPUs.
	sort.Slice(eligibleFreeCompute, func(i, j int) bool { return eligibleFreeCompute[i] > eligibleFreeCompute[j] })
	freeCompute := max(eligibleFreeCompute[requiredDevices-1], 0)

	return common_info.NewFitError(task.Name, task.Namespace, ni.Name, fmt.Sprintf(
		"node %s has enough GPU memory but not enough GPU compute (requested %s, free %s)",
		ni.Name, resource_info.FormatGpuCompute(compute), resource_info.FormatGpuCompute(freeCompute)))
}

func (ni *NodeInfo) enoughResourcesOnGpu(resources *resource_info.GpuResourceRequirement, gpuGroup string) bool {
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
