// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/scheduler_util"
)

func GetTasksToEvict(job *PodGroupInfo, subGroupOrderFn, taskOrderFn common_info.LessFn) ([]*pod_info.PodInfo, bool) {
	reverseTaskOrderFn := func(l interface{}, r interface{}) bool {
		return taskOrderFn(r, l)
	}
	reverseSubGroupOrderFn := func(l interface{}, r interface{}) bool {
		return subGroupOrderFn(r, l)
	}

	return getTasksToEvictWithSubGroups(job, reverseSubGroupOrderFn, reverseTaskOrderFn)
}

func getTasksToEvictWithSubGroups(
	job *PodGroupInfo, reverseSubGroupOrderFn, reverseTaskOrderFn common_info.LessFn,
) ([]*pod_info.PodInfo, bool) {
	subGroupPriorityQueue := getSubGroupsPriorityQueue(job.GetSubGroups(), reverseSubGroupOrderFn)
	maxNumOfSubGroups := getNumOfSubGroupsToEvict(job)

	var tasksToEvict []*pod_info.PodInfo
	numEvictedSubGroups := 0
	for !subGroupPriorityQueue.Empty() && (numEvictedSubGroups < maxNumOfSubGroups) {
		nextSubGroup := subGroupPriorityQueue.Pop().(*subgroup_info.PodSet)

		tasksPriorityQueue := getTasksToEvictPriorityQueue(nextSubGroup, reverseTaskOrderFn)
		maxTasksToEvict := getMaxTasksToEvict(nextSubGroup)
		tasks := getTasksToEvictFromQueue(tasksPriorityQueue, maxTasksToEvict)
		tasksToEvict = append(tasksToEvict, tasks...)
		numEvictedSubGroups += 1
	}

	numAllocatedTasks := job.GetActiveAllocatedTasksCount()
	return tasksToEvict, len(tasksToEvict) < numAllocatedTasks
}

func getNumOfSubGroupsToEvict(podGroupInfo *PodGroupInfo) int {
	for _, subGroup := range podGroupInfo.GetSubGroups() {
		allocatedTasks := subGroup.GetNumActiveAllocatedTasks()

		// If there is at least one subgroup above minAvailable - a single task is evicted
		if allocatedTasks > int(subGroup.GetMinAvailable()) {
			return 1
		}
	}
	return len(podGroupInfo.GetSubGroups())
}

func getMaxTasksToEvict(subGroup *subgroup_info.PodSet) int {
	numAllocatedTasks := subGroup.GetNumActiveAllocatedTasks()
	if numAllocatedTasks > int(subGroup.GetMinAvailable()) {
		return 1
	}
	return numAllocatedTasks
}

func getTasksToEvictPriorityQueue(
	subGroup *subgroup_info.PodSet, taskOrderFn common_info.LessFn,
) *scheduler_util.PriorityQueue {
	podPriorityQueue := scheduler_util.NewPriorityQueue(taskOrderFn, scheduler_util.QueueCapacityInfinite)
	for _, task := range subGroup.GetPodInfos() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			podPriorityQueue.Push(task)
		}
	}
	return podPriorityQueue
}

func getTasksToEvictFromQueue(
	priorityQueue *scheduler_util.PriorityQueue, maxTasksToEvict int,
) []*pod_info.PodInfo {
	numEvictedTasks := 0
	var tasks []*pod_info.PodInfo
	for !priorityQueue.Empty() && (numEvictedTasks < maxTasksToEvict) {
		nextTask := priorityQueue.Pop().(*pod_info.PodInfo)
		tasks = append(tasks, nextTask)
		numEvictedTasks += 1
	}
	return tasks
}
