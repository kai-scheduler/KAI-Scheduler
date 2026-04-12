// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"fmt"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

const (
	RootSubGroupSetName         = ""
	MinimumSubGroupMinAvailable = 0
)

func FromPodGroup(podGroup *v2alpha2.PodGroup) (*SubGroupSet, error) {
	allSubGroups, children, err := mapSubGroupsAndChildren(podGroup)
	if err != nil {
		return nil, err
	}

	var topologyConstraint *topology_info.TopologyConstraintInfo
	if podGroup.Spec.TopologyConstraint.Topology != "" {
		topologyConstraint = &topology_info.TopologyConstraintInfo{
			Topology:       podGroup.Spec.TopologyConstraint.Topology,
			RequiredLevel:  podGroup.Spec.TopologyConstraint.RequiredTopologyLevel,
			PreferredLevel: podGroup.Spec.TopologyConstraint.PreferredTopologyLevel,
		}
	}
	root := NewSubGroupSet(RootSubGroupSetName, topologyConstraint)
	subGroupSets := map[string]*SubGroupSet{
		RootSubGroupSetName: root,
	}
	podSets := map[string]*PodSet{}
	if err := createSubGroupInfos(allSubGroups, children, subGroupSets, podSets); err != nil {
		return nil, err
	}

	err = addToParent(allSubGroups, subGroupSets, podSets)
	if err != nil {
		return nil, err
	}

	return root, nil
}

func mapSubGroupsAndChildren(podGroup *v2alpha2.PodGroup) (map[string]*v2alpha2.SubGroup, map[string][]string, error) {
	allSubGroups := map[string]*v2alpha2.SubGroup{}
	children := map[string][]string{}

	for _, subGroup := range podGroup.Spec.SubGroups {
		if _, found := allSubGroups[subGroup.Name]; found {
			return nil, nil, fmt.Errorf("subgroup <%s> already exists", subGroup.Name)
		}
		allSubGroups[subGroup.Name] = &subGroup
		parentName := formatParentName(subGroup.Parent)
		children[parentName] = append(children[parentName], subGroup.Name)
	}

	return allSubGroups, children, nil
}

func createSubGroupInfos(allSubGroups map[string]*v2alpha2.SubGroup, children map[string][]string,
	subGroupSets map[string]*SubGroupSet, podSets map[string]*PodSet,
) error {
	for name, subGroup := range allSubGroups {
		var topologyConstrainInfo *topology_info.TopologyConstraintInfo
		if subGroup.TopologyConstraint != nil {
			topologyConstrainInfo = &topology_info.TopologyConstraintInfo{
				Topology:       subGroup.TopologyConstraint.Topology,
				RequiredLevel:  subGroup.TopologyConstraint.RequiredTopologyLevel,
				PreferredLevel: subGroup.TopologyConstraint.PreferredTopologyLevel,
			}
		}
		_, hasChildren := children[name]
		if hasChildren {
			subGroupSets[name] = NewSubGroupSet(name, topologyConstrainInfo)
		} else {
			minMember := int32(0)
			if subGroup.MinMember != nil {
				minMember = *subGroup.MinMember
			}
			podSets[name] = NewPodSet(name, max(minMember, MinimumSubGroupMinAvailable), topologyConstrainInfo)
		}
	}
	return nil
}

func addToParent(allSubGroups map[string]*v2alpha2.SubGroup, subGroupSets map[string]*SubGroupSet,
	podSets map[string]*PodSet) error {
	for name, subGroupSet := range subGroupSets {
		if name == RootSubGroupSetName {
			continue
		}

		subGroup := allSubGroups[name]
		if err := addSubGroupSetToParent(subGroupSet, subGroup.Parent, subGroupSets); err != nil {
			return err
		}
	}

	for name, podSet := range podSets {
		subGroup := allSubGroups[name]
		if err := addPodSetToParent(podSet, subGroup.Parent, subGroupSets); err != nil {
			return err
		}
	}

	return nil
}

func addSubGroupSetToParent(subGroupSet *SubGroupSet, parentName *string, subGroupSets map[string]*SubGroupSet) error {
	parent := formatParentName(parentName)
	parentSubGroupSet, found := subGroupSets[parent]
	if !found {
		return fmt.Errorf("parent subgroup <%s> of <%s> not found", parent, subGroupSet.GetName())
	}

	parentSubGroupSet.AddSubGroup(subGroupSet)
	return nil
}

func addPodSetToParent(podSet *PodSet, parentName *string, subGroupSets map[string]*SubGroupSet) error {
	parent := formatParentName(parentName)
	parentSubGroupSet, found := subGroupSets[parent]
	if !found {
		return fmt.Errorf("parent subgroup <%s> of <%s> not found", parent, podSet.GetName())
	}

	parentSubGroupSet.AddPodSet(podSet)
	return nil
}

func formatParentName(parentName *string) string {
	if parentName == nil {
		return RootSubGroupSetName
	}
	return strings.ToLower(*parentName)
}
