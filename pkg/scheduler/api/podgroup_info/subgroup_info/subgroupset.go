// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

type SubGroupSet struct {
	SubGroupInfo

	groups  []*SubGroupSet
	podSets []*PodSet
}

func NewSubGroupSet(name string, topologyConstraint *topology_info.TopologyConstraintInfo) *SubGroupSet {
	subGroupInfo := newSubGroupInfo(name, topologyConstraint)
	return &SubGroupSet{
		SubGroupInfo: *subGroupInfo,
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
