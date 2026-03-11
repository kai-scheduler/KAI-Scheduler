// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

func newNodeInfo(name string, labels map[string]string) *node_info.NodeInfo {
	return &node_info.NodeInfo{
		Name: name,
		Node: &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
		},
	}
}

func TestIsNodePartOfTopology(t *testing.T) {
	levels := []kaiv1alpha1.TopologyLevel{
		{NodeLabel: "zone"},
		{NodeLabel: "rack"},
	}

	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name: "all required labels present",
			labels: map[string]string{
				"zone": "z1",
				"rack": "r1",
			},
			expected: true,
		},
		{
			name: "missing one level label",
			labels: map[string]string{
				"zone": "z1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNodeInfo("node-1", tt.labels)
			assert.Equal(t, tt.expected, isNodePartOfTopology(node, levels))
		})
	}
}

func TestLowestCommonDomainID(t *testing.T) {
	levels := []kaiv1alpha1.TopologyLevel{
		{NodeLabel: "zone"},
		{NodeLabel: "rack"},
	}

	tests := []struct {
		name               string
		nodes              node_info.NodeSet
		topologyConstraint *topology_info.TopologyConstraintInfo
		expectedID         DomainID
		expectedLevel      DomainLevel
		expectedValid      []string
	}{
		{
			name: "all nodes share full topology",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1", "rack": "r1"}),
				newNodeInfo("node-2", map[string]string{"zone": "z1", "rack": "r1"}),
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{},
			expectedID:         DomainID("z1.r1"),
			expectedLevel:      DomainLevel("rack"),
			expectedValid:      []string{"node-1", "node-2"},
		},
		{
			name: "all nodes share full topology - but the preferred level is zone",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1", "rack": "r1"}),
				newNodeInfo("node-2", map[string]string{"zone": "z1", "rack": "r1"}),
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				PreferredLevel: "zone",
			},
			expectedID:    DomainID("z1"),
			expectedLevel: DomainLevel("zone"),
			expectedValid: []string{"node-1", "node-2"},
		},
		{
			name: "mismatch at deeper level returns common prefix",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1", "rack": "r1"}),
				newNodeInfo("node-2", map[string]string{"zone": "z1", "rack": "r2"}),
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{},
			expectedID:         DomainID("z1"),
			expectedLevel:      DomainLevel("zone"),
			expectedValid:      []string{"node-1", "node-2"},
		},
		{
			name: "mismatch at first level returns root",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1", "rack": "r1"}),
				newNodeInfo("node-2", map[string]string{"zone": "z2", "rack": "r1"}),
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{},
			expectedID:         DomainID(rootDomainId),
			expectedLevel:      DomainLevel(rootLevel),
			expectedValid:      []string{"node-1", "node-2"},
		},
		{
			name: "invalid nodes are filtered out",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1", "rack": "r1"}),
				newNodeInfo("node-2", map[string]string{"zone": "z1"}), // missing rack
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{},
			expectedID:         DomainID("z1.r1"),
			expectedLevel:      DomainLevel("rack"),
			expectedValid:      []string{"node-1"},
		},
		{
			name: "no valid nodes returns root and empty map",
			nodes: node_info.NodeSet{
				newNodeInfo("node-1", map[string]string{"zone": "z1"}), // missing rack
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{},
			expectedID:         DomainID(rootDomainId),
			expectedLevel:      DomainLevel(rootLevel),
			expectedValid:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domainID, domainLevel, valid := lowestCommonDomainID(tt.nodes, levels, tt.topologyConstraint)

			assert.Equal(t, tt.expectedID, domainID)
			assert.Equal(t, tt.expectedLevel, domainLevel)

			if len(tt.expectedValid) == 0 {
				assert.Empty(t, valid)
				return
			}

			assert.Len(t, valid, len(tt.expectedValid))
			for _, nodeName := range tt.expectedValid {
				assert.Contains(t, valid, nodeName)
			}
		})
	}
}
