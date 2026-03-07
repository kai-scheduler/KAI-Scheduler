// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"strings"

	kaiv1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/topology_info"
)

// lowestCommonDomainID returns the lowest common domain ID, level, and valid (=in domain) nodes for a given node
// set and levels in descending order (like in the topology CRD). If a node is missing one of the levels, the function
// will assume it's outside the topology and it will not be included in the valid nodes map.
func lowestCommonDomainID(nodeSet node_info.NodeSet, levels []kaiv1alpha1.TopologyLevel,
	topologyConstraint *topology_info.TopologyConstraintInfo) (DomainID, DomainLevel, map[string]*node_info.NodeInfo) {
	validNodes := map[string]*node_info.NodeInfo{}
	for _, node := range nodeSet {
		if !isNodePartOfTopology(node, levels) {
			continue
		}
		validNodes[node.Name] = node
	}

	var leastCommonLevel string
	if len(topologyConstraint.PreferredLevel) > 0 {
		leastCommonLevel = topologyConstraint.PreferredLevel
	}

	var domainParts []string
	for _, level := range levels {
		allMatch := true
		var value string
		for _, node := range validNodes {
			newValue := node.Node.Labels[level.NodeLabel]

			if value == "" {
				value = newValue
			}

			if newValue != value {
				allMatch = false
				break
			}
		}

		if !allMatch || value == "" {
			break
		}

		domainParts = append(domainParts, value)

		if leastCommonLevel == level.NodeLabel {
			// There is no reason to try and find a lower level then the preferred level because
			//  it's the lowest level that we will look at for the topology allocation
			break
		}
	}

	if len(domainParts) == 0 {
		return rootDomainId, rootLevel, validNodes
	}

	return DomainID(strings.Join(domainParts, ".")), DomainLevel(levels[len(domainParts)-1].NodeLabel), validNodes
}

// For a given node to be part of the topology correctly, it must have a label for each level of the topology. TODO make this common
func isNodePartOfTopology(nodeInfo *node_info.NodeInfo, levels []kaiv1alpha1.TopologyLevel) bool {
	for _, level := range levels {
		if _, found := nodeInfo.Node.Labels[level.NodeLabel]; !found {
			return false
		}
	}
	return true
}
