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
// (its "core"): at each SubGroupSet the coreMin() members returned by coreMembers, recursively; at
// each leaf PodSet the coreMin() highest-priority allocated pods (sorted by taskOrderFn). The
// remaining allocated tasks are elastic surplus.
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

	k := coreMin(sgs)
	if k > len(members) {
		k = len(members)
	}
	return members[:k], members[k:]
}

// coreMin is the number of m's members that hold core slots: minNonPreemptible when the PodGroup set
// it on this node, otherwise the node's gang minimum. minNonPreemptible may only raise the count, so
// the core is always a superset of the gang and protecting it never breaks the gang.
func coreMin(m subgroup_info.SubGroupMember) int {
	if minNonPreemptible := m.GetMinNonPreemptible(); minNonPreemptible != nil {
		return int(*minNonPreemptible)
	}
	return m.GetMinMembersToSatisfy()
}

// coreMemberNames returns the names of the members holding sgs's core slots.
func coreMemberNames(sgs *subgroup_info.SubGroupSet) map[string]bool {
	names := map[string]bool{}
	for _, member := range coreMembers(sgs) {
		names[member.GetName()] = true
	}
	return names
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

// collectCoreFromPodSet adds the coreMin highest-priority allocated pods of a leaf PodSet to core.
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

	minMembers := coreMin(ps)
	for i := 0; i < minMembers && i < len(allocated); i++ {
		core[allocated[i].UID] = allocated[i]
	}
}

// IsMinRequirementSatisfied reports whether the job's core is fully allocated, i.e. any further
// allocation is elastic burst. Reads the core thresholds, which minNonPreemptible may raise above the
// gang minimums; with minNonPreemptible unset this is the root's IsMinRequirementSatisfied term for
// term. The per-member check matters for flat jobs, where the override sits on the default PodSet
// rather than the root, so comparing root arity alone would report satisfied at the first pod.
func IsMinRequirementSatisfied(job *PodGroupInfo) bool {
	root := rootSubGroupSet(job)
	satisfied := 0
	for _, member := range root.GetMembers() {
		if member.GetNumActiveAllocatedMembers() >= coreMin(member) {
			satisfied++
		}
	}
	return satisfied >= coreMin(root)
}
