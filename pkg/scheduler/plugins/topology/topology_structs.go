// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	kueuev1alpha1 "sigs.k8s.io/kueue/apis/kueue/v1alpha1"
)

// TopologyDomainID uniquely identifies a topology domain
type TopologyDomainID string

// TopologyInfo represents a topology tree for the cluster
type TopologyInfo struct {
	// Root of the topology tree
	Root *TopologyDomainInfo

	// Map of all domains by their level for quick lookup
	DomainsByLevel map[string]map[TopologyDomainID]*TopologyDomainInfo

	// Name of this topology configuration
	Name string

	// Topology resource
	TopologyResource *kueuev1alpha1.Topology
}

// TopologyDomainInfo represents a node in the topology tree
type TopologyDomainInfo struct {
	// Unique ID of this domain
	ID TopologyDomainID

	// Name of this domain
	Name string

	// Level in the hierarchy (e.g., "datacenter", "zone", "rack", "node")
	Level string

	// Parent domain, nil for root
	Parent *TopologyDomainInfo

	// Child domains
	Children map[TopologyDomainID]*TopologyDomainInfo

	// Nodes that belong to this domain
	Nodes map[string]*node_info.NodeInfo

	// Number of pods that can be allocated in this domain for the job
	AllocatablePods int

	// Depth in the tree from root (0 for root)
	Depth int
}

func NewTopologyDomainInfo(id TopologyDomainID, name, level string, depth int) *TopologyDomainInfo {
	return &TopologyDomainInfo{
		ID:       id,
		Name:     name,
		Level:    level,
		Parent:   nil,
		Children: map[TopologyDomainID]*TopologyDomainInfo{},
		Nodes:    map[string]*node_info.NodeInfo{},
		Depth:    depth,
	}
}

func (t *TopologyDomainInfo) AddNode(nodeInfo *node_info.NodeInfo) {
	t.Nodes[nodeInfo.Node.Name] = nodeInfo
}

func calcDomainId(leafLevelIndex int, levels []kueuev1alpha1.TopologyLevel, nodeLabels map[string]string) TopologyDomainID {
	domainsNames := make([]string, leafLevelIndex+1)
	for levelIndex := leafLevelIndex; levelIndex >= 0; levelIndex-- {
		levelLabel := levels[levelIndex].NodeLabel
		levelDomainName, foundLevelOnNode := nodeLabels[levelLabel]
		if !foundLevelOnNode {
			levelDomainName = "missing"
		}
		domainsNames[levelIndex] = levelDomainName
	}
	return TopologyDomainID(strings.Join(domainsNames, "."))
}

func connectDomainToParent(domain *TopologyDomainInfo, parent *TopologyDomainInfo) {
	domain.Parent = parent
	parent.Children[domain.ID] = domain
}
