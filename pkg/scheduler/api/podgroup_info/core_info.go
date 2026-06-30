// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

// GetCoreTasks returns the active-allocated pods that form the job's minimal satisfying set
// (the non-preemptible "core"), honoring minSubGroup at every SubGroupSet and minMember at every
// leaf PodSet. Surplus subgroups and surplus leaf pods (the elastic tier) are excluded.
//
// The result mirrors the set eviction protects (see eviction_info.go): quota accounting and
// eviction agree on what "core" means by construction. For a flat job (no minSubGroup) this is
// exactly the per-PodSet minMember count.
func GetCoreTasks(
	job *PodGroupInfo, subGroupOrderFn common_info.LessFn, taskOrderFn common_info.LessFn,
) map[common_info.PodID]*pod_info.PodInfo {
	core := map[common_info.PodID]*pod_info.PodInfo{}

	root := job.RootSubGroupSet
	if root == nil {
		root = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		for _, ps := range job.PodSets {
			root.AddPodSet(ps)
		}
	}

	collectCoreFromSubGroupSet(root, subGroupOrderFn, taskOrderFn, core)
	return core
}

// IsMinRequirementSatisfied reports whether the job's root has met its minimal satisfying set (the
// gang phase is complete). Once satisfied, any further allocation is elastic — used by quota checks
// to decide whether tasks about to be allocated count as core (gang phase) or elastic (beyond it).
func IsMinRequirementSatisfied(job *PodGroupInfo) bool {
	root := job.RootSubGroupSet
	if root == nil {
		root = subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		for _, ps := range job.PodSets {
			root.AddPodSet(ps)
		}
	}
	return root.IsMinRequirementSatisfied()
}

// collectCoreFromSubGroupSet adds the core (non-preemptible) tasks of a SubGroupSet subtree to core.
func collectCoreFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, subGroupOrderFn, taskOrderFn common_info.LessFn,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	members := sgs.GetMembers()
	k := sgs.GetMinMembersToSatisfy()

	if sgs.GetNumActiveAllocatedDirectSubGroups() <= k {
		// Gang not over-satisfied: every member is needed to reach (or hold) the minimum.
		// Surplus exists only deeper (leaf pods beyond minAvailable), handled by recursion.
		for _, member := range members {
			collectCoreFromMember(member, subGroupOrderFn, taskOrderFn, core)
		}
		return
	}

	// Over-satisfied: only the k highest-priority satisfied members are core; the rest are elastic.
	satisfied := make([]subgroup_info.SubGroupMember, 0, len(members))
	for _, member := range members {
		if member.GetNumActiveAllocatedMembers() >= member.GetMinMembersToSatisfy() {
			satisfied = append(satisfied, member)
		}
	}
	sort.Slice(satisfied, func(i, j int) bool {
		return subGroupOrderFn(satisfied[i], satisfied[j])
	})
	for i := 0; i < k && i < len(satisfied); i++ {
		collectCoreFromMember(satisfied[i], subGroupOrderFn, taskOrderFn, core)
	}
}

func collectCoreFromMember(
	member subgroup_info.SubGroupMember, subGroupOrderFn, taskOrderFn common_info.LessFn,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		collectCoreFromSubGroupSet(m, subGroupOrderFn, taskOrderFn, core)
	case *subgroup_info.PodSet:
		collectCoreFromPodSet(m, taskOrderFn, core)
	}
}

// collectCoreFromPodSet adds the minAvailable highest-priority active-allocated pods of a leaf to core.
func collectCoreFromPodSet(
	ps *subgroup_info.PodSet, taskOrderFn common_info.LessFn,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	allocated := make([]*pod_info.PodInfo, 0, len(ps.GetPodInfos()))
	for _, task := range ps.GetPodInfos() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			allocated = append(allocated, task)
		}
	}
	sort.Slice(allocated, func(i, j int) bool {
		return taskOrderFn(allocated[i], allocated[j])
	})

	minAvailable := int(ps.GetMinAvailable())
	for i := 0; i < minAvailable && i < len(allocated); i++ {
		core[allocated[i].UID] = allocated[i]
	}
}
