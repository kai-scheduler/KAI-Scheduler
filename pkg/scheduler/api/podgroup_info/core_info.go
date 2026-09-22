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
	collectCoreFromSubGroupSet(rootSubGroupSet(job), taskOrderFn, pinnedCoreMembers(job), core)
	return core
}

// pinnedCoreMembers returns the names of the members that already hold a core slot, derived from the
// core pod set the scheduler last published to the PodGroup status.
//
// Core membership must not move once established. coreMemberLess ranks satisfied members first, so
// without a pin a subgroup that only fills up later can displace the incumbent: the job's
// not-preemptible charge then jumps to the new member's minimum, even though admission approved that
// allocation as elastic burst and charged it nothing (see coreRequiredQuota). Returns nil before the
// first status write, where recomputation is the only available answer.
func pinnedCoreMembers(job *PodGroupInfo) map[string]bool {
	if job.PodGroup == nil || job.PodGroup.Status.SchedulingState == nil {
		return nil
	}
	corePodNames := map[string]bool{}
	for _, name := range job.PodGroup.Status.SchedulingState.CorePods {
		corePodNames[name] = true
	}
	if len(corePodNames) == 0 {
		return nil
	}
	pinned := map[string]bool{}
	markPinnedMembers(rootSubGroupSet(job), corePodNames, pinned)
	return pinned
}

// markPinnedMembers records every member holding at least one published core pod, and reports
// whether sgs itself holds one so ancestors are pinned along with their descendants.
func markPinnedMembers(sgs *subgroup_info.SubGroupSet, corePodNames, pinned map[string]bool) bool {
	holdsCorePod := false
	for _, member := range sgs.GetMembers() {
		memberHolds := false
		switch m := member.(type) {
		case *subgroup_info.SubGroupSet:
			memberHolds = markPinnedMembers(m, corePodNames, pinned)
		case *subgroup_info.PodSet:
			for _, task := range m.GetPodInfos() {
				if corePodNames[task.Name] {
					memberHolds = true
					break
				}
			}
		}
		if memberHolds {
			pinned[member.GetName()] = true
			holdsCorePod = true
		}
	}
	return holdsCorePod
}

// coreMembers returns the members occupying sgs's core slots: members already pinned by the last
// published core set first, then satisfied members, then by name.
// Single source of truth for core membership — core collection recurses into these, and the
// semi-preemptible eviction path refuses to evict them.
//
// This is deliberately NOT the session's SubGroupOrderFn. That function answers "which subgroup
// should get the next pod?" and so ranks unsatisfied members first, which is exactly inverted for
// protection: it would hand a core slot to a half-filled or even empty subgroup while leaving a
// complete one elastic. The pin, not the name, is what makes the set sticky across sessions: name
// ordering alone still lets a member that becomes satisfied later jump ahead of the incumbent. If
// membership moved as pods came and went, eviction would chase it and unravel the gang one subgroup
// at a time, and quota accounting would recharge a different minimum each session. See the design
// doc's "Core Selection Ordering".
func coreMembers(sgs *subgroup_info.SubGroupSet, pinned map[string]bool) []subgroup_info.SubGroupMember {
	core, _ := partitionCoreMembers(sgs, pinned)
	return core
}

// partitionCoreMembers splits sgs's members into those holding core slots and the rest, both sorted
// by coreMemberLess.
func partitionCoreMembers(
	sgs *subgroup_info.SubGroupSet, pinned map[string]bool,
) (core, nonCore []subgroup_info.SubGroupMember) {
	members := sgs.GetMembers()
	sort.Slice(members, func(i, j int) bool {
		return coreMemberLess(members[i], members[j], pinned)
	})

	k := sgs.GetMinMembersToSatisfy()
	if k > len(members) {
		k = len(members)
	}
	return members[:k], members[k:]
}

func coreMemberLess(l, r subgroup_info.SubGroupMember, pinned map[string]bool) bool {
	lPinned, rPinned := holdsPinnedCoreSlot(l, pinned), holdsPinnedCoreSlot(r, pinned)
	if lPinned != rPinned {
		return lPinned
	}
	lSatisfied, rSatisfied := isMemberSatisfied(l), isMemberSatisfied(r)
	if lSatisfied != rSatisfied {
		return lSatisfied
	}
	return l.GetName() < r.GetName()
}

// holdsPinnedCoreSlot reports whether member keeps the core slot it held last session. The slot is
// released once the member has nothing allocated, so a core member that loses every pod stops
// shielding the rest of the job's surplus from eviction.
func holdsPinnedCoreSlot(member subgroup_info.SubGroupMember, pinned map[string]bool) bool {
	return pinned[member.GetName()] && member.GetNumActiveAllocatedMembers() > 0
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
	sgs *subgroup_info.SubGroupSet, taskOrderFn common_info.LessFn, pinned map[string]bool,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	for _, member := range coreMembers(sgs, pinned) {
		collectCoreFromMember(member, taskOrderFn, pinned, core)
	}
}

func collectCoreFromMember(
	member subgroup_info.SubGroupMember, taskOrderFn common_info.LessFn, pinned map[string]bool,
	core map[common_info.PodID]*pod_info.PodInfo,
) {
	switch m := member.(type) {
	case *subgroup_info.SubGroupSet:
		collectCoreFromSubGroupSet(m, taskOrderFn, pinned, core)
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
