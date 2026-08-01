// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

func TestPodGroupInfo_ResolveTopologyAliases(t *testing.T) {
	aliasesByTopology := map[string]map[string]string{
		"network": {
			"rack": "accelerator.nvidia.com/rack",
			"node": "kubernetes.io/hostname",
		},
	}

	root := subgroup_info.NewSubGroupSet("",
		&topology_info.TopologyConstraintInfo{Topology: "network", RequiredLevel: "rack"})
	child := subgroup_info.NewSubGroupSet("child",
		&topology_info.TopologyConstraintInfo{Topology: "network", PreferredLevel: "node"})
	podSet := subgroup_info.NewPodSet("leaf", 1,
		&topology_info.TopologyConstraintInfo{Topology: "network", RequiredLevel: "node"})
	child.AddPodSet(podSet)
	root.AddSubGroup(child)

	pgi := &PodGroupInfo{RootSubGroupSet: root}
	pgi.ResolveTopologyAliases(aliasesByTopology)

	assert.Equal(t, "accelerator.nvidia.com/rack", root.GetTopologyConstraint().RequiredLevel)
	assert.Equal(t, "kubernetes.io/hostname", child.GetTopologyConstraint().PreferredLevel)
	assert.Equal(t, "kubernetes.io/hostname", podSet.GetTopologyConstraint().RequiredLevel)
}

func TestPodGroupInfo_ResolveTopologyAliases_PassThroughAndOtherTopology(t *testing.T) {
	aliasesByTopology := map[string]map[string]string{
		"network": {"rack": "accelerator.nvidia.com/rack"},
	}

	// Raw label (not an alias) and a constraint for a topology with no aliases both pass through.
	root := subgroup_info.NewSubGroupSet("",
		&topology_info.TopologyConstraintInfo{Topology: "network", RequiredLevel: "accelerator.nvidia.com/rack"})
	other := subgroup_info.NewSubGroupSet("other",
		&topology_info.TopologyConstraintInfo{Topology: "zone-topology", RequiredLevel: "rack"})
	root.AddSubGroup(other)

	pgi := &PodGroupInfo{RootSubGroupSet: root}
	pgi.ResolveTopologyAliases(aliasesByTopology)

	assert.Equal(t, "accelerator.nvidia.com/rack", root.GetTopologyConstraint().RequiredLevel)
	assert.Equal(t, "rack", other.GetTopologyConstraint().RequiredLevel, "unknown topology ⇒ unchanged")
}

func TestPodGroupInfo_ResolveTopologyAliases_NoAliasesNoop(t *testing.T) {
	root := subgroup_info.NewSubGroupSet("",
		&topology_info.TopologyConstraintInfo{Topology: "network", RequiredLevel: "rack"})
	pgi := &PodGroupInfo{RootSubGroupSet: root}

	pgi.ResolveTopologyAliases(map[string]map[string]string{}) // no aliases anywhere
	assert.Equal(t, "rack", root.GetTopologyConstraint().RequiredLevel)
}
