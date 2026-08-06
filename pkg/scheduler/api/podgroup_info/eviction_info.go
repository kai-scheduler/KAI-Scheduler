// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"slices"
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

	root := rootSubGroupSet(job)

	var tasks []*pod_info.PodInfo
	if job.IsSemiPreemptibleJob() {
		// Semi-preemptible jobs offer only their elastic surplus as victims; the core (minimal
		// satisfying shape) is never evicted, so the phase-3 full-eviction fallback is skipped.
		// Orphans go first: they are neither surplus nor core, so no elastic phase can reach them.
		tasks = collectOrphanEviction(root, reverseTaskOrderFn)
		if len(tasks) == 0 {
			tasks = collectElasticEvictionFromSubGroupSet(root, reverseSubGroupOrderFn, reverseTaskOrderFn)
		}

		// The collection above ranks members by the allocation ordering, which is not the ordering
		// GetCoreTasks protects by. Today the two happen to agree, but only as a side effect of phase 1
		// consuming every above-threshold member first. Filter against the core set so agreement is a
		// guarantee: dropping a batch entirely is the safe direction, since under-evicting only leaves
		// surplus behind while evicting a core task breaks the gang.
		core := GetCoreTasks(job, taskOrderFn)
		tasks = slices.DeleteFunc(tasks, func(task *pod_info.PodInfo) bool {
			_, isCore := core[task.UID]
			return isCore
		})
	} else {
		tasks = collectTasksToEvictFromSubGroupSet(root, reverseSubGroupOrderFn, reverseTaskOrderFn)
	}

	jobHasMoreActiveTasksAfterEviction := len(tasks) < job.GetActiveAllocatedTasksCount()
	return tasks, jobHasMoreActiveTasksAfterEviction
}

// collectOrphanEviction returns the allocated tasks of a member that holds no core slot and has not
// formed its gang. Those pods deliver nothing - no gang of their own, no core slot - so a
// semi-preemptible job gives them up before any real surplus. Recurses into core SubGroupSets, where
// the same rule applies one level down.
func collectOrphanEviction(
	sgs *subgroup_info.SubGroupSet, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	// A job still reaching its minimum has its partial members filling core slots by name, and any
	// beyond that fill are still growing towards it. Nothing is an orphan until the gang has formed.
	if !sgs.IsMinRequirementSatisfied() {
		return nil
	}

	core, nonCore := partitionCoreMembers(sgs)
	for _, member := range nonCore {
		if isMemberSatisfied(member) {
			continue // real surplus - phases 1 and 2 own it
		}
		if tasks := collectGangEvictionFromMember(member, reverseTaskOrderFn); len(tasks) > 0 {
			return tasks
		}
	}
	for _, member := range core {
		if sub, ok := member.(*subgroup_info.SubGroupSet); ok {
			if tasks := collectOrphanEviction(sub, reverseTaskOrderFn); len(tasks) > 0 {
				return tasks
			}
		}
	}
	return nil
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

	members := sgs.GetMembers()
	sort.Slice(members, func(i, j int) bool {
		return reverseSubGroupOrderFn(members[i], members[j])
	})

	// Phase 1 — Elastic recursive: look for elastic surplus deeper in the tree.
	if hasElasticSurplusInSubGroupSet(sgs) {
		for _, member := range members {
			tasks := collectElasticEvictionFromMember(member, reverseSubGroupOrderFn, reverseTaskOrderFn)
			if len(tasks) > 0 {
				return tasks
			}
		}
	}

	// Phase 2 — Elastic direct: drop least-prioritized member entirely if sgs has surplus members.
	if sgs.GetMinMembersToSatisfy() < numSatisfied {
		for _, member := range members {
			tasks := collectGangEvictionFromMember(member, reverseTaskOrderFn)
			if len(tasks) > 0 {
				return tasks
			}
		}
	}

	return nil
}

func collectElasticEvictionFromMember(
	member subgroup_info.SubGroupMember, reverseSubGroupOrderFn, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		return collectElasticEvictionFromSubGroupSet(m, reverseSubGroupOrderFn, reverseTaskOrderFn)
	case *subgroup_info.PodSet:
		return collectElasticEvictionFromPodSet(m, reverseTaskOrderFn)
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

// collectGangEvictionFromMember collects all allocated tasks from a member in the context of its parent's gang phase.
// If we reach a gang eviction of a given SubGroupMember, it means that all the pods under this subtree needs to be evicted.
// Any elastic pods / subgroups (if they existed and have an active status) have been evicted in previous phases.
func collectGangEvictionFromMember(
	member subgroup_info.SubGroupMember, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		return collectAllAllocatedTasksFromSubGroupSet(m, reverseTaskOrderFn)
	case *subgroup_info.PodSet:
		return collectAllAllocatedTasksFromPodSet(m, reverseTaskOrderFn)
	}
	return nil
}

func collectAllAllocatedTasksFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, reverseTaskOrderFn common_info.LessFn,
) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, ps := range sgs.GetDescendantPodSets() {
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
	if sgs.GetNumActiveAllocatedDirectSubGroups() > sgs.GetMinMembersToSatisfy() {
		return true
	}
	for _, member := range sgs.GetMembers() {
		if hasElasticSurplusInMember(member) {
			return true
		}
	}
	return false
}

func hasElasticSurplusInMember(member subgroup_info.SubGroupMember) bool {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		return hasElasticSurplusInSubGroupSet(m)
	case *subgroup_info.PodSet:
		return m.GetNumActiveAllocatedTasks() > int(m.GetMinAvailable())
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
