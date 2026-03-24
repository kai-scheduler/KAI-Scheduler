// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"math"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func HasTasksToAllocate(podGroupInfo *PodGroupInfo, isRealAllocation bool) bool {
	for _, task := range podGroupInfo.GetAllPodsMap() {
		if task.ShouldAllocate(isRealAllocation) {
			return true
		}
	}
	return false
}

func GetTasksToAllocate(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	isRealAllocation bool,
) []*pod_info.PodInfo {
	if podGroupInfo.tasksToAllocate != nil {
		return podGroupInfo.tasksToAllocate
	}

	var tasksToAllocate []*pod_info.PodInfo
	subGroupPriorityQueue := getSubGroupsPriorityQueue(podGroupInfo.GetSubGroups(), subGroupOrderFn)
	maxNumSubGroups := getMaxNumSubGroupsToAllocate(podGroupInfo)
	numSubGroupsToAllocate := 0

	for !subGroupPriorityQueue.Empty() && (numSubGroupsToAllocate < maxNumSubGroups) {
		nextSubGroup := subGroupPriorityQueue.Pop().(*subgroup_info.PodSet)
		taskPriorityQueue := getTasksPriorityQueue(nextSubGroup, taskOrderFn, isRealAllocation)
		if taskPriorityQueue.Empty() {
			continue
		}
		maxNumOfTasksToAllocate := getNumTasksToAllocate(nextSubGroup, isRealAllocation)
		subGroupTasks := getTasksFromQueue(taskPriorityQueue, maxNumOfTasksToAllocate)
		tasksToAllocate = append(tasksToAllocate, subGroupTasks...)
		numSubGroupsToAllocate += 1
	}

	podGroupInfo.tasksToAllocate = tasksToAllocate
	return tasksToAllocate
}

func GetTasksToAllocateRequestedGPUs(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	isRealAllocation bool,
) (float64, int64) {
	tasksTotalRequestedGPUs := float64(0)
	tasksTotalRequestedGpuMemory := int64(0)
	for _, task := range GetTasksToAllocate(podGroupInfo, subGroupOrderFn, taskOrderFn, isRealAllocation) {
		tasksTotalRequestedGPUs += task.ResReq.GPUs()
		tasksTotalRequestedGpuMemory += task.ResReq.GpuMemory()

		for _, draGpuCount := range task.ResReq.GpuResourceRequirement.DraGpuCounts() {
			tasksTotalRequestedGPUs += float64(draGpuCount)
			// Currently, we do not support DRA gpu memory requests.
			// DRA gpu requests that have memory constraints (e.g. 2 gpus, each with at least 32GB) are supported by adding the device count (e.g. 2) to the total requested GPUs.
			// This is calculated in the same way that whole gpus are added to the total requested GPUs.
			tasksTotalRequestedGpuMemory += 0
		}

		for migResource, quant := range task.ResReq.MigResources() {
			gpuPortion, mem, err := resources.ExtractGpuAndMemoryFromMigResourceName(migResource.String())
			if err != nil {
				log.InfraLogger.Errorf("failed to evaluate device portion for resource %v: %v", migResource, err)
				continue
			}
			tasksTotalRequestedGPUs += float64(int64(gpuPortion) * quant)
			tasksTotalRequestedGpuMemory += int64(mem) * quant
		}
	}

	return tasksTotalRequestedGPUs, tasksTotalRequestedGpuMemory
}

func GetTasksToAllocateInitResource(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	isRealAllocation bool, minNodeGPUMemory int64,
) *resource_info.Resource {
	if podGroupInfo == nil {
		return resource_info.EmptyResource()
	}
	if podGroupInfo.tasksToAllocateInitResource != nil {
		return podGroupInfo.tasksToAllocateInitResource
	}

	tasksTotalRequestedResource := resource_info.EmptyResource()
	for _, task := range GetTasksToAllocate(podGroupInfo, subGroupOrderFn, taskOrderFn, isRealAllocation) {
		if task.ShouldAllocate(isRealAllocation) {
			tasksTotalRequestedResource.AddResourceRequirements(task.ResReq)
			if task.IsMemoryRequest() && minNodeGPUMemory > 0 {
				additionalGpuFraction := float64(task.ResReq.GpuResourceRequirement.GetNumOfGpuDevices()) *
					(float64(task.ResReq.GpuMemory()) / float64(minNodeGPUMemory))
				tasksTotalRequestedResource.AddGPUs(additionalGpuFraction)
			}
		}
	}

	podGroupInfo.tasksToAllocateInitResource = tasksTotalRequestedResource
	return tasksTotalRequestedResource
}

func GetTasksToAllocateInitResourceVector(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	isRealAllocation bool, minNodeGPUMemory int64,
) resource_info.ResourceVector {
	if podGroupInfo == nil {
		return nil
	}
	if podGroupInfo.tasksToAllocateInitResourceVector != nil {
		return podGroupInfo.tasksToAllocateInitResourceVector
	}

	result := resource_info.NewResourceVector(podGroupInfo.VectorMap)
	gpuIdx := podGroupInfo.VectorMap.GetIndex("gpu")
	for _, task := range GetTasksToAllocate(podGroupInfo, subGroupOrderFn, taskOrderFn, isRealAllocation) {
		if task.ShouldAllocate(isRealAllocation) {
			result.Add(task.ResReqVector)
			if task.IsMemoryRequest() && minNodeGPUMemory > 0 {
				additionalGpuFraction := float64(task.ResReq.GpuResourceRequirement.GetNumOfGpuDevices()) *
					(float64(task.ResReq.GpuMemory()) / float64(minNodeGPUMemory))
				result.Set(gpuIdx, result.Get(gpuIdx)+additionalGpuFraction)
			}
		}
	}

	podGroupInfo.tasksToAllocateInitResourceVector = result
	return result
}

func getTasksPriorityQueue(
	subGroup *subgroup_info.PodSet, taskOrderFn common_info.LessFn, isRealAllocation bool,
) *scheduler_util.PriorityQueue {
	priorityQueue := scheduler_util.NewPriorityQueue(taskOrderFn, scheduler_util.QueueCapacityInfinite)
	for _, task := range subGroup.GetPodInfos() {
		if task.ShouldAllocate(isRealAllocation) {
			priorityQueue.Push(task)
		}
	}
	return priorityQueue
}

func getTasksFromQueue(priorityQueue *scheduler_util.PriorityQueue, maxNumTasks int) []*pod_info.PodInfo {
	var tasksToAllocate []*pod_info.PodInfo
	for !priorityQueue.Empty() && (len(tasksToAllocate) < maxNumTasks) {
		nextPod := priorityQueue.Pop().(*pod_info.PodInfo)
		tasksToAllocate = append(tasksToAllocate, nextPod)
	}
	return tasksToAllocate
}

func getSubGroupsPriorityQueue(subGroups map[string]*subgroup_info.PodSet,
	subGroupOrderFn common_info.LessFn) *scheduler_util.PriorityQueue {
	priorityQueue := scheduler_util.NewPriorityQueue(subGroupOrderFn, scheduler_util.QueueCapacityInfinite)
	for _, subGroup := range subGroups {
		priorityQueue.Push(subGroup)
	}
	return priorityQueue
}

func getNumTasksToAllocate(subGroup *subgroup_info.PodSet, isRealAllocation bool) int {
	numAllocatedTasks := subGroup.GetNumActiveAllocatedTasks()
	if numAllocatedTasks >= int(subGroup.GetMinAvailable()) {
		numTasksToAllocate := getNumAllocatableTasks(subGroup, isRealAllocation)
		return int(math.Min(float64(numTasksToAllocate), 1))
	} else {
		return int(subGroup.GetMinAvailable()) - numAllocatedTasks
	}
}

func getNumAllocatableTasks(subGroup *subgroup_info.PodSet, isRealAllocation bool) int {
	numTasksToAllocate := 0
	for _, task := range subGroup.GetPodInfos() {
		if task.ShouldAllocate(isRealAllocation) {
			numTasksToAllocate += 1
		}
	}
	return numTasksToAllocate
}

func getMaxNumSubGroupsToAllocate(podGroupInfo *PodGroupInfo) int {
	numUnsatisfied := 0
	for _, subGroup := range podGroupInfo.GetSubGroups() {
		allocatedTasks := subGroup.GetNumActiveAllocatedTasks()
		if allocatedTasks < int(subGroup.GetMinAvailable()) {
			numUnsatisfied += 1
		}
	}
	if numUnsatisfied > 0 {
		return numUnsatisfied
	}
	return 1
}
