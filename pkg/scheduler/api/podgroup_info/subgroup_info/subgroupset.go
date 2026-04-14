// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

type SubGroupSet struct {
	SubGroupInfo

	minSubGroup *int32
	groups      []*SubGroupSet
	podSets     []*PodSet
}

func NewSubGroupSet(name string, topologyConstraint *topology_info.TopologyConstraintInfo) *SubGroupSet {
	subGroupInfo := newSubGroupInfo(name, topologyConstraint)
	return &SubGroupSet{
		SubGroupInfo: *subGroupInfo,
		minSubGroup:  nil,
		groups:       []*SubGroupSet{},
		podSets:      []*PodSet{},
	}
}

func (sgs *SubGroupSet) AddSubGroup(subGroup *SubGroupSet) {
	subGroup.SetParent(sgs)
	sgs.groups = append(sgs.groups, subGroup)
}

func (sgs *SubGroupSet) AddPodSet(podSet *PodSet) {
	podSet.SetParent(sgs)
	sgs.podSets = append(sgs.podSets, podSet)
}

func (sgs *SubGroupSet) GetChildGroups() []*SubGroupSet {
	return sgs.groups
}

func (sgs *SubGroupSet) GetChildPodSets() []*PodSet {
	return sgs.podSets
}

func (sgs *SubGroupSet) Clone() *SubGroupSet {
	root := NewSubGroupSet(sgs.name, sgs.topologyConstraint)
	root.SetMinSubGroup(sgs.minSubGroup)
	for _, podSet := range sgs.podSets {
		clonePodSet := podSet.Clone()
		root.AddPodSet(clonePodSet)
	}
	for _, subGroup := range sgs.groups {
		childSubGroup := subGroup.Clone()
		root.AddSubGroup(childSubGroup)
	}
	return root
}

func (sgs *SubGroupSet) GetAllPodSets() map[string]*PodSet {
	result := make(map[string]*PodSet)
	for _, podSet := range sgs.podSets {
		result[podSet.GetName()] = podSet
	}
	for _, subGroup := range sgs.groups {
		podSets := subGroup.GetAllPodSets()
		for name, podSet := range podSets {
			result[name] = podSet
		}
	}
	return result
}

func (sgs *SubGroupSet) SetParent(parent *SubGroupSet) {
	sgs.parent = parent
}

func (sgs *SubGroupSet) GetParent() *SubGroupSet {
	return sgs.parent
}

func (sgs *SubGroupSet) GetMinSubGroup() *int32 {
	return sgs.minSubGroup
}

func (sgs *SubGroupSet) SetMinSubGroup(minSubGroup *int32) {
	sgs.minSubGroup = minSubGroup
}

func (sgs *SubGroupSet) GetNumActiveAllocatedDirectSubGroups() int {
	count := 0
	for _, child := range sgs.GetChildGroups() {
		if child.GetNumActiveAllocatedDirectSubGroups() >= child.GetMinChildrenToSatisfy() {
			count++
		}
	}
	for _, podSet := range sgs.GetChildPodSets() {
		if podSet.GetNumActiveAllocatedTasks() >= int(podSet.GetMinAvailable()) {
			count++
		}
	}
	return count
}

func (sgs *SubGroupSet) GetMinChildrenToSatisfy() int {
	if minSG := sgs.GetMinSubGroup(); minSG != nil {
		return int(*minSG)
	}
	return len(sgs.GetChildGroups()) + len(sgs.GetChildPodSets())
}

func (sgs *SubGroupSet) GetChildren() []SubGroupChild {
	children := make([]SubGroupChild, 0, len(sgs.groups)+len(sgs.podSets))
	for _, childSubgroup := range sgs.groups {
		children = append(children, childSubgroup)
	}
	for _, childSubgroup := range sgs.podSets {
		children = append(children, childSubgroup)
	}
	return children
}

func (sgs *SubGroupSet) IsMinRequirementSatisfied() bool {
	return sgs.GetNumActiveAllocatedDirectSubGroups() >= sgs.GetMinChildrenToSatisfy()
}

func (sgs *SubGroupSet) GetNumActiveAllocatedDirectChildren() int {
	return sgs.GetNumActiveAllocatedDirectSubGroups()
}
