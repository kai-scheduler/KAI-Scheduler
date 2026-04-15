// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func GetTasksToEvict(job *PodGroupInfo, subGroupOrderFn, taskOrderFn common_info.LessFn) ([]*pod_info.PodInfo, bool) {
	reverseTaskOrderFn := func(l interface{}, r interface{}) bool {
		return taskOrderFn(r, l)
	}
	reverseSubGroupOrderFn := func(l interface{}, r interface{}) bool {
		return subGroupOrderFn(r, l)
	}

	root := job.RootSubGroupSet
	if root == nil {
		root = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		for _, ps := range job.PodSets {
			root.AddPodSet(ps)
		}
	}

	tasks := collectTasksToEvictFromSubGroupSet(root, reverseSubGroupOrderFn, reverseTaskOrderFn)

	jobHasMoreActiveTasksAfterEviction := len(tasks) < job.GetActiveAllocatedTasksCount()
	return tasks, jobHasMoreActiveTasksAfterEviction
}

// collectTasksToEvictFromSubGroupSet runs phases 1+2 (elastic), then falls back to phase 3 (full eviction).
func collectTasksToEvictFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, reverseSubGroupOrderFn, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	tasks := collectElasticEvictionFromSubGroupSet(sgs, reverseSubGroupOrderFn, reverseTaskOrderFn)
	if len(tasks) > 0 {
		return tasks
	}
	return collectAllAllocatedTasksFromSubGroupSet(sgs, reverseTaskOrderFn)
}

// collectElasticEvictionFromSubGroupSet runs phases 1+2 only, returns nil if no elastic surplus.
func collectElasticEvictionFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, reverseSubGroupOrderFn, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	numSatisfied := sgs.GetNumActiveAllocatedDirectSubGroups()
	if numSatisfied == 0 {
		return nil
	}

	children := sgs.GetChildren()
	sort.Slice(children, func(i, j int) bool {
		return reverseSubGroupOrderFn(children[i], children[j])
	})

	// Phase 1 — Elastic recursive: look for elastic surplus deeper in the tree.
	if hasElasticSurplusInSubGroupSet(sgs) {
		for _, child := range children {
			tasks := collectElasticEvictionFromChild(child, reverseSubGroupOrderFn, reverseTaskOrderFn)
			if len(tasks) > 0 {
				return tasks
			}
		}
	}

	// Phase 2 — Elastic direct: drop least-prioritized child entirely if sgs has surplus children.
	if sgs.GetMinChildrenToSatisfy() < numSatisfied {
		for _, child := range children {
			tasks := collectGangEvictionFromChild(child, reverseTaskOrderFn)
			if len(tasks) > 0 {
				return tasks
			}
		}
	}

	return nil
}

func collectElasticEvictionFromChild(
	child subgroup_info.SubGroupChild, reverseSubGroupOrderFn, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	switch c := child.(type) {
	case *subgroup_info.SubGroupSet:
		return collectElasticEvictionFromSubGroupSet(c, reverseSubGroupOrderFn, reverseTaskOrderFn)
	case *subgroup_info.PodSet:
		return collectElasticEvictionFromPodSet(c, reverseTaskOrderFn)
	}
	return nil
}

func collectElasticEvictionFromPodSet(
	ps *subgroup_info.PodSet, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	if ps.GetNumActiveAllocatedTasks() <= int(ps.GetMinAvailable()) {
		return nil
	}
	taskQueue := getEvictableTasksPriorityQueue(ps, reverseTaskOrderFn)
	return getTasksFromQueue(taskQueue, 1)
}

// collectGangEvictionFromChild collects all allocated tasks from a child in the context of its parent's gang phase.
// If we reach a gang eviction of a given SubGroupChild, it means that all the pods under this subtree needs to be evicted.
// Any elastic pods / subgroups (if they existed and have an active status) have been evicted in previous phases.
func collectGangEvictionFromChild(
	child subgroup_info.SubGroupChild, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	switch c := child.(type) {
	case *subgroup_info.SubGroupSet:
		return collectAllAllocatedTasksFromSubGroupSet(c, reverseTaskOrderFn)
	case *subgroup_info.PodSet:
		return collectAllAllocatedTasksFromPodSet(c, reverseTaskOrderFn)
	}
	return nil
}

func collectAllAllocatedTasksFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, ps := range sgs.GetAllPodSets() {
		tasks = append(tasks, collectAllAllocatedTasksFromPodSet(ps, reverseTaskOrderFn)...)
	}
	return tasks
}

func collectAllAllocatedTasksFromPodSet(
	ps *subgroup_info.PodSet, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	taskQueue := getEvictableTasksPriorityQueue(ps, reverseTaskOrderFn)
	return getTasksFromQueue(taskQueue, taskQueue.Len())
}

func hasElasticSurplusInSubGroupSet(sgs *subgroup_info.SubGroupSet) bool {
	if sgs.GetNumActiveAllocatedDirectSubGroups() > sgs.GetMinChildrenToSatisfy() {
		return true
	}
	for _, child := range sgs.GetChildren() {
		if hasElasticSurplusInChild(child) {
			return true
		}
	}
	return false
}

func hasElasticSurplusInChild(child subgroup_info.SubGroupChild) bool {
	switch c := child.(type) {
	case *subgroup_info.SubGroupSet:
		return hasElasticSurplusInSubGroupSet(c)
	case *subgroup_info.PodSet:
		return c.GetNumActiveAllocatedTasks() > int(c.GetMinAvailable())
	}
	return false
}

func getEvictableTasksPriorityQueue(
	ps *subgroup_info.PodSet, reverseTaskOrderFn common_info.LessFn,
) *scheduler_util.PriorityQueue {
	podPriorityQueue := scheduler_util.NewPriorityQueue(reverseTaskOrderFn, scheduler_util.QueueCapacityInfinite)
	for _, task := range ps.GetPodInfos() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			podPriorityQueue.Push(task)
		}
	}
	return podPriorityQueue
}
