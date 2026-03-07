// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"strings"

	kaiv1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1alpha1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

const (
	allocatablePodsNotSet = -1
)

// DomainID uniquely identifies a topology domain
type DomainID string

type DomainLevel string

type LevelDomainInfos map[DomainID]*DomainInfo

type domainsByLevel map[DomainLevel]LevelDomainInfos

// Info represents a topology tree for the cluster
type Info struct {
	// Map of all domains by their level for quick lookup
	DomainsByLevel domainsByLevel

	// Name of this topology configuration
	Name string

	// Topology resource
	TopologyResource *kaiv1alpha1.Topology
}

// DomainInfo represents a node in the topology tree
type DomainInfo struct {
	// Unique ID of this domain
	ID DomainID

	// Level in the hierarchy (e.g., "datacenter", "zone", "rack", "node")
	Level DomainLevel

	// Child domains (slice maintains ordering for bin-packing and traversal algorithms)
	Children []*DomainInfo

	// Nodes that belong to this domain
	Nodes map[string]*node_info.NodeInfo

	// Number of pods that can be allocated in this domain for the job
	AllocatablePods int

	// Total available resources in this domain
	IdleOrReleasingResources *resource_info.Resource
}

func NewDomainInfo(id DomainID, level DomainLevel) *DomainInfo {
	return &DomainInfo{
		ID:                       id,
		Level:                    level,
		Children:                 []*DomainInfo{},
		Nodes:                    map[string]*node_info.NodeInfo{},
		AllocatablePods:          allocatablePodsNotSet,
		IdleOrReleasingResources: resource_info.EmptyResource(),
	}
}

func (di *DomainInfo) AddNode(nodeInfo *node_info.NodeInfo) {
	di.Nodes[nodeInfo.Name] = nodeInfo
}

func (di *DomainInfo) GetNonAllocatedGPUsInDomain() float64 {
	result := float64(0)
	for _, node := range di.Nodes {
		result += node.NonAllocatedResource(resource_info.GPUResourceName)
	}
	return result
}

func (t *DomainInfo) AddChild(child *DomainInfo) {
	// Check if child already exists to avoid duplicates
	for _, existingChild := range t.Children {
		if existingChild.ID == child.ID {
			return
		}
	}
	t.Children = append(t.Children, child)
}

func calcDomainId(leafLevelIndex int, levels []kaiv1alpha1.TopologyLevel, nodeLabels map[string]string) DomainID {
	domainsNames := make([]string, leafLevelIndex+1)
	for levelIndex := leafLevelIndex; levelIndex >= 0; levelIndex-- {
		levelLabel := levels[levelIndex].NodeLabel
		domainsNames[levelIndex] = nodeLabels[levelLabel]
	}
	return DomainID(strings.Join(domainsNames, "."))
}
