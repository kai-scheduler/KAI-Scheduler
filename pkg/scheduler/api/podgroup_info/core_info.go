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

// GetCoreTasks returns the set of allocated tasks that make up the job's minimal satisfying shape
// (its "core"): at each SubGroupSet the GetMinMembersToSatisfy() members returned by coreMembers,
// recursively; at each leaf PodSet the minAvailable highest-priority allocated pods (sorted by
// taskOrderFn). The remaining allocated tasks are elastic surplus.
//
// Flat jobs (no minSubGroup) reduce to the per-leaf-minMember result and are backward compatible.
func GetCoreTasks(
	job *PodGroupInfo, taskOrderFn common_info.LessFn,
) map[common_info.PodID]*pod_info.PodInfo {
	core := map[common_info.PodID]*pod_info.PodInfo{}
	collectCoreFromSubGroupSet(rootSubGroupSet(job), taskOrderFn, core)
	return core
}

// coreMembers returns the members occupying sgs's core slots: satisfied members first, then by name.
// Single source of truth for core membership — core collection recurses into these, and the
// semi-preemptible eviction path refuses to evict them.
//
// This is deliberately NOT the session's SubGroupOrderFn. That function answers "which subgroup
// should get the next pod?" and so ranks unsatisfied members first, which is exactly inverted for
// protection: it would hand a core slot to a half-filled or even empty subgroup while leaving a
// complete one elastic. Name, not allocation, breaks ties so the core set is sticky across sessions;
// if it moved as pods came and went, eviction would chase it and unravel the gang one subgroup at a
// time. See the design doc's "Core Selection Ordering".
func coreMembers(sgs *subgroup_info.SubGroupSet) []subgroup_info.SubGroupMember {
	core, _ := partitionCoreMembers(sgs)
	return core
}

// partitionCoreMembers splits sgs's members into those holding core slots and the rest, both sorted
// by coreMemberLess.
func partitionCoreMembers(sgs *subgroup_info.SubGroupSet) (core, nonCore []subgroup_info.SubGroupMember) {
	members := sgs.GetMembers()
	sort.Slice(members, func(i, j int) bool {
		return coreMemberLess(members[i], members[j])
	})

	k := sgs.GetMinMembersToSatisfy()
	if k > len(members) {
		k = len(members)
	}
	return members[:k], members[k:]
}

func coreMemberLess(l, r subgroup_info.SubGroupMember) bool {
	lSatisfied, rSatisfied := isMemberSatisfied(l), isMemberSatisfied(r)
	if lSatisfied != rSatisfied {
		return lSatisfied
	}
	return l.GetName() < r.GetName()
}

func isMemberSatisfied(member subgroup_info.SubGroupMember) bool {
	return member.GetNumActiveAllocatedMembers() >= member.GetMinMembersToSatisfy()
}

// rootSubGroupSet returns the job's root SubGroupSet, synthesizing one from its PodSets for flat jobs.
func rootSubGroupSet(job *PodGroupInfo) *subgroup_info.SubGroupSet {
	if job.RootSubGroupSet != nil {
		return job.RootSubGroupSet
	}
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	for _, ps := range job.PodSets {
		root.AddPodSet(ps)
	}
	return root
}

// GetCorePodNames returns the pod names of GetCoreTasks, sorted, so the published set is stable
// across sessions and can be compared for equality.
func GetCorePodNames(job *PodGroupInfo, taskOrderFn common_info.LessFn) []string {
	core := GetCoreTasks(job, taskOrderFn)
	names := make([]string, 0, len(core))
	for _, task := range core {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	return names
}

// collectCoreFromSubGroupSet adds the core tasks of a SubGroupSet to the accumulator. Applied at every
// level: a core subgroup protects only its own core children, so surplus nested inside a protected
// subtree stays elastic.
func collectCoreFromSubGroupSet(
	sgs *subgroup_info.SubGroupSet, taskOrderFn common_info.LessFn,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	for _, member := range coreMembers(sgs) {
		collectCoreFromMember(member, taskOrderFn, core)
	}
}

func collectCoreFromMember(
	member subgroup_info.SubGroupMember, taskOrderFn common_info.LessFn,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		collectCoreFromSubGroupSet(m, taskOrderFn, core)
	case *subgroup_info.PodSet:
		collectCoreFromPodSet(m, taskOrderFn, core)
	}
}

// collectCoreFromPodSet adds the minAvailable highest-priority allocated pods of a leaf PodSet to core.
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

	minMembers := ps.GetMinMembersToSatisfy()
	for i := 0; i < minMembers && i < len(allocated); i++ {
		core[allocated[i].UID] = allocated[i]
	}
}

// IsMinRequirementSatisfied reports whether the job's root SubGroupSet has met its minimal shape,
// i.e. the whole core is allocated and any further allocation is elastic burst.
func IsMinRequirementSatisfied(job *PodGroupInfo) bool {
	return rootSubGroupSet(job).IsMinRequirementSatisfied()
}
