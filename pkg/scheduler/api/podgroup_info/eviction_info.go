// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func GetTasksToEvict(job *PodGroupInfo, taskOrderFn common_info.LessFn) ([]*pod_info.PodInfo, bool) {
	reverseTaskOrderFn := func(l interface{}, r interface{}) bool {
		return taskOrderFn(r, l)
	}
	podPriorityQueue := scheduler_util.NewPriorityQueue(reverseTaskOrderFn, scheduler_util.QueueCapacityInfinite)
	for _, task := range job.PodInfos {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			podPriorityQueue.Push(task)
		}
	}

	if podPriorityQueue.Empty() {
		return []*pod_info.PodInfo{}, false
	}

	if int(job.MinAvailable) < podPriorityQueue.Len() {
		task := podPriorityQueue.Pop().(*pod_info.PodInfo)
		return []*pod_info.PodInfo{task}, true
	}

	var tasksToEvict []*pod_info.PodInfo
	for !podPriorityQueue.Empty() {
		nextTask := podPriorityQueue.Pop().(*pod_info.PodInfo)
		tasksToEvict = append(tasksToEvict, nextTask)
	}

	return tasksToEvict, false
}
