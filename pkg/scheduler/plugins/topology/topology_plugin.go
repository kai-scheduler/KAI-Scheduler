// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const (
	topologyPluginName = "topology"
	rootLevel          = "root"
	rootDomainId       = rootLevel
)

type topologyName = string
type subgroupName = string

type topologyPlugin struct {
	TopologyTrees map[topologyName]*Info

	// Defines order among nodes in a sub-group based on the sub-group's preferred level topology constraint.
	subGroupNodeScores map[subgroupName]map[string]float64
	session            *framework.Session
}

func New(_ framework.PluginArguments) framework.Plugin {
	return &topologyPlugin{
		TopologyTrees:      map[topologyName]*Info{},
		subGroupNodeScores: map[subgroupName]map[string]float64{},
		session:            nil,
	}
}

func (t *topologyPlugin) Name() string {
	return topologyPluginName
}

func (t *topologyPlugin) OnSessionOpen(ssn *framework.Session) {
	t.session = ssn
	t.initializeTopologyTree(ssn.ClusterInfo.Topologies, ssn.ClusterInfo.Nodes)

	ssn.AddSubsetNodesFn(t.subSetNodesFn)
	ssn.AddNodeOrderFn(t.nodeOrderFn)
	ssn.AddPreJobAllocationFn(t.preJobAllocationFn)
}

func (t *topologyPlugin) preJobAllocationFn(_ *podgroup_info.PodGroupInfo) {
	// Invalidate the sub-group node scores
	t.subGroupNodeScores = map[subgroupName]map[string]float64{}
}

func (t *topologyPlugin) initializeTopologyTree(topologies []*kaiv1alpha1.Topology, nodes map[string]*node_info.NodeInfo) {
	for _, topology := range topologies {
		topologyTree := &Info{
			Name: topology.Name,
			DomainsByLevel: map[DomainLevel]LevelDomainInfos{
				rootLevel: {
					rootDomainId: NewDomainInfo(rootDomainId, rootLevel),
				},
			},
			TopologyResource: topology,
		}

		for _, nodeInfo := range nodes {
			t.addNodeDataToTopology(topologyTree, topology, nodeInfo)
		}

		t.TopologyTrees[topology.Name] = topologyTree
	}
}

func (*topologyPlugin) addNodeDataToTopology(topologyTree *Info, topology *kaiv1alpha1.Topology, nodeInfo *node_info.NodeInfo) {
	// Validate that the node is part of the topology
	if !isNodePartOfTopology(nodeInfo, topology.Spec.Levels) {
		return
	}

	var nodeContainingChildDomain *DomainInfo
	for levelIndex := len(topology.Spec.Levels) - 1; levelIndex >= 0; levelIndex-- {
		level := topology.Spec.Levels[levelIndex]

		domainId := calcDomainId(levelIndex, topology.Spec.Levels, nodeInfo.Node.Labels)
		domainLevel := DomainLevel(level.NodeLabel)
		domainsForLevel, foundLevelLabel := topologyTree.DomainsByLevel[domainLevel]
		if !foundLevelLabel {
			topologyTree.DomainsByLevel[domainLevel] = map[DomainID]*DomainInfo{}
			domainsForLevel = topologyTree.DomainsByLevel[domainLevel]
		}
		domainInfo, foundDomain := domainsForLevel[domainId]
		if !foundDomain {
			domainInfo = NewDomainInfo(domainId, domainLevel)
			domainsForLevel[domainId] = domainInfo
		}
		domainInfo.AddNode(nodeInfo)

		// Connect the child domain to the current domain. The current node gives us the link
		if nodeContainingChildDomain != nil {
			domainInfo.AddChild(nodeContainingChildDomain)
		}
		nodeContainingChildDomain = domainInfo
	}

	topologyTree.DomainsByLevel[rootLevel][rootDomainId].AddChild(nodeContainingChildDomain)
	topologyTree.DomainsByLevel[rootLevel][rootDomainId].AddNode(nodeInfo)
}

func (t *topologyPlugin) OnSessionClose(_ *framework.Session) {}
