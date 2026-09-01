// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"math"
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

// TaskAllocationMode identifies why tasks are being selected for allocation.
type TaskAllocationMode int

type taskAllocationCacheMode int

const (
	// RealTaskAllocation selects pending tasks for binding in the current scheduling cycle.
	RealTaskAllocation TaskAllocationMode = iota
	// SimulatedTaskAllocation selects pending tasks while evaluating a scheduling scenario.
	SimulatedTaskAllocation
	// PartialTaskAllocation selects partial task sets for internal search and resource accounting.
	PartialTaskAllocation
	// VictimReallocation selects tasks from a preempted job while restoring its remaining allocation.
	VictimReallocation
)

const (
	realTaskAllocationCache taskAllocationCacheMode = iota
	simulatedAdmissionCache
	partialTaskAllocationCache
)

func (mode TaskAllocationMode) isRealAllocation() bool {
	return mode == RealTaskAllocation
}

func (mode TaskAllocationMode) enforceGangAdmission() bool {
	return mode == RealTaskAllocation || mode == SimulatedTaskAllocation
}

func (mode TaskAllocationMode) cacheMode() taskAllocationCacheMode {
	switch mode {
	case RealTaskAllocation:
		return realTaskAllocationCache
	case SimulatedTaskAllocation:
		return simulatedAdmissionCache
	default:
		return partialTaskAllocationCache
	}
}

func HasTasksToAllocate(podGroupInfo *PodGroupInfo, mode TaskAllocationMode) bool {
	for _, task := range podGroupInfo.GetAllPodsMap() {
		if task.ShouldAllocate(mode.isRealAllocation()) {
			return true
		}
	}
	return false
}

// GetTasksToAllocate returns the tasks that should be allocated for the given pod group info, sorted by the given order functions.
// The tasks are collected from all subgroups of the podgroup, respecting the minAvailable and minSubgroup constraints.
// For satisfied subgroups, collect tasks only from one direct child (a single subgroup for a minSubgroup or a single pod for a minAvailable).
func GetTasksToAllocate(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode,
) []*pod_info.PodInfo {
	cacheMode := mode.cacheMode()
	if tasks, ok := podGroupInfo.tasksToAllocateByMode[cacheMode]; ok {
		return tasks
	}

	if podGroupInfo.RootSubGroupSet == nil {
		return nil
	}
	tasks := collectTasksFromSubGroupSet(podGroupInfo.RootSubGroupSet, subGroupOrderFn, taskOrderFn, mode)
	if podGroupInfo.tasksToAllocateByMode == nil {
		podGroupInfo.tasksToAllocateByMode = make(map[taskAllocationCacheMode][]*pod_info.PodInfo)
	}
	podGroupInfo.tasksToAllocateByMode[cacheMode] = tasks
	return tasks
}

// collectTasksFromSubGroupSet walks the SubGroupSet tree and collects tasks to allocate.
func collectTasksFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode,
) []*pod_info.PodInfo {
	if mode.enforceGangAdmission() && !sgs.IsReadyForScheduling() {
		return nil
	}

	K := sgs.GetMinMembersToSatisfy()
	children := sgs.GetMembers()
	sort.Slice(children, func(i, j int) bool {
		return subGroupOrderFn(children[i], children[j])
	})

	if !sgs.IsMinRequirementSatisfied() {
		membersToCollect := K - sgs.GetNumActiveAllocatedDirectSubGroups()
		var tasks []*pod_info.PodInfo
		for i := 0; i < len(children) && membersToCollect > 0; i++ {
			childTasks := collectFromChildInGangPhase(children[i], subGroupOrderFn, taskOrderFn, mode)
			if len(childTasks) == 0 {
				continue
			}
			tasks = append(tasks, childTasks...)
			membersToCollect--
		}
		return tasks
	}

	// Elastic phase: get the most prioritized unsatisfied child, and allocate it
	for i := 0; i < len(children); i++ {
		childTasks := collectFromChildSubgroup(children[i], subGroupOrderFn, taskOrderFn, mode)
		if len(childTasks) > 0 {
			return childTasks
		}
	}
	return nil
}

// collectFromChildInGangPhase collects tasks from a child in the context of its parent's gang phase.
// SubGroupSets recurse normally; satisfied PodSets are skipped because their gang requirement
// is already met and collecting elastic tasks from them would over-count resource needs.
func collectFromChildInGangPhase(
	child subgroup_info.SubGroupMember, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode,
) []*pod_info.PodInfo {
	switch c := child.(type) {
	case *subgroup_info.SubGroupSet:
		if c.IsMinRequirementSatisfied() {
			return nil // already satisfied; skip in parent gang phase
		}
		return collectTasksFromSubGroupSet(c, subGroupOrderFn, taskOrderFn, mode)
	case *subgroup_info.PodSet:
		if c.GetNumActiveAllocatedTasks() >= int(c.GetMinAvailable()) {
			return nil // already satisfied; skip in parent gang phase
		}
		return collectTasksFromPodSet(c, taskOrderFn, mode)
	}
	return nil
}

func collectFromChildSubgroup(
	child subgroup_info.SubGroupMember, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode,
) []*pod_info.PodInfo {
	switch c := child.(type) {
	case *subgroup_info.SubGroupSet:
		return collectTasksFromSubGroupSet(c, subGroupOrderFn, taskOrderFn, mode)
	case *subgroup_info.PodSet:
		return collectTasksFromPodSet(c, taskOrderFn, mode)
	}
	return nil
}

func collectTasksFromPodSet(ps *subgroup_info.PodSet, taskOrderFn common_info.LessFn, mode TaskAllocationMode) []*pod_info.PodInfo {
	taskPriorityQueue := getTasksPriorityQueue(ps, taskOrderFn, mode.isRealAllocation())
	if taskPriorityQueue.Empty() {
		return nil
	}
	numTasksToAllocate := getNumTasksToAllocate(ps, mode.isRealAllocation())
	if mode.enforceGangAdmission() && ps.GetNumActiveAllocatedTasks() < int(ps.GetMinAvailable()) && taskPriorityQueue.Len() < numTasksToAllocate {
		return nil
	}
	return getTasksFromQueue(taskPriorityQueue, numTasksToAllocate)
}

func GetTasksToAllocateRequestedGPUs(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode,
) (float64, int64) {
	tasksTotalRequestedGPUs := float64(0)
	tasksTotalRequestedGpuMemory := int64(0)
	for _, task := range GetTasksToAllocate(podGroupInfo, subGroupOrderFn, taskOrderFn, mode) {
		tasksTotalRequestedGPUs += task.GpuRequirement.GPUs()
		tasksTotalRequestedGpuMemory += task.GpuRequirement.GpuMemory()

		for _, draGpuCount := range task.GpuRequirement.DraGpuCounts() {
			tasksTotalRequestedGPUs += float64(draGpuCount)
			// Currently, we do not support DRA gpu memory requests.
			// DRA gpu requests that have memory constraints (e.g. 2 gpus, each with at least 32GB) are supported by adding the device count (e.g. 2) to the total requested GPUs.
			// This is calculated in the same way that whole gpus are added to the total requested GPUs.
			tasksTotalRequestedGpuMemory += 0
		}

		for migResource, quant := range task.GpuRequirement.MigResources() {
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

// GetTasksToAllocateInitResourceVector returns the aggregated resource vector of the tasks to
// allocate, converting GPU-memory requests to a GPU fraction using minNodeGPUMemory (the smallest
// per-GPU memory in the cluster) as a conservative upper-bound divisor; nil minNodeGPUMemory skips
// the conversion (no node advertises GPU memory, so such requests are unschedulable anyway).
// The result is cached on the PodGroupInfo and the cache does NOT key on the divisor: this function
// must only ever be called with the min divisor. Quota/limit accounting that needs a different
// divisor (e.g. capacity_policy's max) must compute its own conversion, not route through here.
func GetTasksToAllocateInitResourceVector(
	podGroupInfo *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
	mode TaskAllocationMode, minNodeGPUMemory *int64,
) resource_info.ResourceVector {
	if podGroupInfo == nil {
		return nil
	}
	cacheMode := mode.cacheMode()
	if result, ok := podGroupInfo.tasksToAllocateInitResourceVectorByMode[cacheMode]; ok {
		return result
	}

	result := resource_info.NewResourceVector(podGroupInfo.VectorMap)
	gpuIdx := resource_info.GPUIndex
	for _, task := range GetTasksToAllocate(podGroupInfo, subGroupOrderFn, taskOrderFn, mode) {
		if task.ShouldAllocate(mode.isRealAllocation()) {
			result.Add(task.ResReqVector)
			if task.IsGpuMemoryRequest() && minNodeGPUMemory != nil {
				result.Set(gpuIdx, result.Get(gpuIdx)+task.GpuRequirement.GpuMemoryAsGpuFraction(*minNodeGPUMemory))
			}
		}
	}

	if podGroupInfo.tasksToAllocateInitResourceVectorByMode == nil {
		podGroupInfo.tasksToAllocateInitResourceVectorByMode = make(map[taskAllocationCacheMode]resource_info.ResourceVector)
	}
	podGroupInfo.tasksToAllocateInitResourceVectorByMode[cacheMode] = result
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
