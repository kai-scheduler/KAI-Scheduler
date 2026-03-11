// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetLevelDomains(t *testing.T) {
	tests := []struct {
		name          string
		root          *DomainInfo
		level         DomainLevel
		expectedCount int
		expectedIDs   []DomainID
	}{
		{
			name: "root at target level",
			root: &DomainInfo{
				ID:    "zone1",
				Level: "zone",
			},
			level:         "zone",
			expectedCount: 1,
			expectedIDs:   []DomainID{"zone1"},
		},
		{
			name: "no children at target level",
			root: &DomainInfo{
				ID:       "zone1",
				Level:    "zone",
				Children: []*DomainInfo{},
			},
			level:         "rack",
			expectedCount: 0,
		},
		{
			name: "multiple children at target level",
			root: &DomainInfo{
				ID:    "zone1",
				Level: "zone",
				Children: []*DomainInfo{
					{ID: "rack1", Level: "rack"},
					{ID: "rack2", Level: "rack"},
					{ID: "rack3", Level: "rack"},
				},
			},
			level:         "rack",
			expectedCount: 3,
			expectedIDs:   []DomainID{"rack1", "rack2", "rack3"},
		},
		{
			name: "nested hierarchy - target at deepest level",
			root: &DomainInfo{
				ID:    "region1",
				Level: "region",
				Children: []*DomainInfo{
					{
						ID:    "zone1",
						Level: "zone",
						Children: []*DomainInfo{
							{ID: "rack1", Level: "rack"},
							{ID: "rack2", Level: "rack"},
						},
					},
					{
						ID:    "zone2",
						Level: "zone",
						Children: []*DomainInfo{
							{ID: "rack3", Level: "rack"},
						},
					},
				},
			},
			level:         "rack",
			expectedCount: 3,
			expectedIDs:   []DomainID{"rack1", "rack2", "rack3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLevelDomains(tt.root, tt.level)
			assert.Equal(t, tt.expectedCount, len(result))

			if tt.expectedIDs != nil {
				resultIDs := make([]DomainID, len(result))
				for i, d := range result {
					resultIDs[i] = d.ID
				}
				assert.ElementsMatch(t, tt.expectedIDs, resultIDs)
			}
		})
	}
}

func TestCalculateNodeScores(t *testing.T) {
	tests := []struct {
		name           string
		domain         *DomainInfo
		preferredLevel DomainLevel
		expectedScores map[string]float64
	}{
		{
			name: "single rack with nodes",
			domain: &DomainInfo{
				ID:    "rack1",
				Level: "rack",
				Nodes: map[string]*node_info.NodeInfo{
					"node1": {Name: "node1", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
					"node2": {Name: "node2", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}},
				},
			},
			preferredLevel: "rack",
			expectedScores: map[string]float64{
				"node1": 10.0 * scores.Topology,
				"node2": 10.0 * scores.Topology,
			},
		},
		{
			name: "multiple racks - scores decrease by position",
			domain: &DomainInfo{
				ID:    "zone1",
				Level: "zone",
				Children: []*DomainInfo{
					{
						ID:              "rack3",
						Level:           "rack",
						AllocatablePods: 1,
						Nodes: map[string]*node_info.NodeInfo{
							"node3": {Name: "node3", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}},
						},
					},
					{
						ID:              "rack2",
						Level:           "rack",
						AllocatablePods: 2,
						Nodes: map[string]*node_info.NodeInfo{
							"node2": {Name: "node2", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}},
						},
					},
					{
						ID:              "rack1",
						Level:           "rack",
						AllocatablePods: 3,
						Nodes: map[string]*node_info.NodeInfo{
							"node1": {Name: "node1", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
						},
					},
				},
			},
			preferredLevel: "rack",
			expectedScores: map[string]float64{
				"node1": 10.0 * scores.Topology, // First rack (3/3)
				"node2": 6.0 * scores.Topology,  // Second rack (2/3)
				"node3": 3.0 * scores.Topology,  // Third rack (1/3)
			},
		},
		{
			name: "4 racks - verify scoring distribution",
			domain: &DomainInfo{
				ID:    "zone1",
				Level: "zone",
				Children: []*DomainInfo{
					{
						ID:    "rack4",
						Level: "rack",
						Nodes: map[string]*node_info.NodeInfo{
							"node4": {Name: "node4", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node4"}}},
						},
					},
					{
						ID:    "rack3",
						Level: "rack",
						Nodes: map[string]*node_info.NodeInfo{
							"node3": {Name: "node3", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}},
						},
					},
					{
						ID:    "rack2",
						Level: "rack",
						Nodes: map[string]*node_info.NodeInfo{
							"node2": {Name: "node2", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}},
						},
					},
					{
						ID:    "rack1",
						Level: "rack",
						Nodes: map[string]*node_info.NodeInfo{
							"node1": {Name: "node1", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
						},
					},
				},
			},
			preferredLevel: "rack",
			expectedScores: map[string]float64{
				"node1": 10.0 * scores.Topology, // 4/4 = 1.0 * 10 = 10.0
				"node2": 7.0 * scores.Topology,  // 3/4 = 0.75 * 10 = 7.5 -> floor = 7.0
				"node3": 5.0 * scores.Topology,  // 2/4 = 0.5 * 10 = 5.0
				"node4": 2.0 * scores.Topology,  // 1/4 = 0.25 * 10 = 2.5 -> floor = 2.0
			},
		},
		{
			name: "nested hierarchy - nodes directly in preferred level domains",
			domain: &DomainInfo{
				ID:    "region1",
				Level: "region",
				Children: []*DomainInfo{
					{
						ID:    "zone2",
						Level: "zone",
						Nodes: map[string]*node_info.NodeInfo{
							"node3": {Name: "node3", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}},
							"node4": {Name: "node4", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node4"}}},
						},
					},
					{
						ID:    "zone1",
						Level: "zone",
						Nodes: map[string]*node_info.NodeInfo{
							"node1": {Name: "node1", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
							"node2": {Name: "node2", Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}},
						},
					},
				},
			},
			preferredLevel: "zone",
			expectedScores: map[string]float64{
				"node1": 10.0 * scores.Topology, // First zone
				"node2": 10.0 * scores.Topology, // First zone
				"node3": 5.0 * scores.Topology,  // Second zone
				"node4": 5.0 * scores.Topology,  // Second zone
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := calculateNodeScores(tt.domain, tt.preferredLevel)
			assert.Equal(t, len(tt.expectedScores), len(scores))
			for nodeName, expectedScore := range tt.expectedScores {
				actualScore, exists := scores[nodeName]
				require.True(t, exists, "Expected score for node %s not found", nodeName)
				assert.InDelta(t, expectedScore, actualScore, 0.001, "Score mismatch for node %s", nodeName)
			}
		})
	}
}

func TestSortTree(t *testing.T) {
	tests := []struct {
		name           string
		tasksResources *resource_info.Resource
		setupTree      func() *DomainInfo
		maxDepthLevel  DomainLevel
		expectedOrder  []DomainID
		checkLevel     DomainLevel
	}{
		{
			name:           "nil root",
			tasksResources: nil,
			setupTree: func() *DomainInfo {
				return nil
			},
			maxDepthLevel: "rack",
			expectedOrder: nil,
		},
		{
			name:           "empty max depth level",
			tasksResources: resource_info.NewResource(0, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "zone1",
					Level: "zone",
					Children: []*DomainInfo{
						{ID: "rack1", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(0, 0, 1)},
						{ID: "rack2", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(0, 0, 1)},
					},
				}
			},
			maxDepthLevel: "",
			expectedOrder: nil,
		},
		{
			name:           "sort single level - gpus",
			tasksResources: resource_info.NewResource(0, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "zone1",
					Level: "zone",
					Children: []*DomainInfo{
						{ID: "rack3", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
						{ID: "rack1", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(0, 0, 2)},
						{ID: "rack2", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(0, 0, 8)},
					},
				}
			},
			maxDepthLevel: "rack",
			expectedOrder: []DomainID{"rack1", "rack3", "rack2"},
			checkLevel:    "zone",
		},
		{
			name:           "sort single level - cpu",
			tasksResources: resource_info.NewResource(1000, 0, 0),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "zone1",
					Level: "zone",
					Children: []*DomainInfo{
						{ID: "rack3", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(5000, 0, 0)},
						{ID: "rack1", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(2000, 0, 0)},
						{ID: "rack2", Level: "rack", IdleOrReleasingResources: resource_info.NewResource(8000, 0, 0)},
					},
				}
			},
			maxDepthLevel: "rack",
			expectedOrder: []DomainID{"rack1", "rack3", "rack2"},
			checkLevel:    "zone",
		},
		{
			name:           "sort single level - several dominant resources",
			tasksResources: resource_info.NewResource(1000, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "zone1",
					Level: "zone",
					Children: []*DomainInfo{
						{ID: "rack3", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(100000, 0, 5)},
						{ID: "rack1", Level: "rack", AllocatablePods: 2, IdleOrReleasingResources: resource_info.NewResource(2000, 0, 4)},
						{ID: "rack2", Level: "rack", AllocatablePods: 8, IdleOrReleasingResources: resource_info.NewResource(100000, 0, 8)},
					},
				}
			},
			maxDepthLevel: "rack",
			expectedOrder: []DomainID{"rack1", "rack3", "rack2"},
			checkLevel:    "zone",
		},
		{
			name:           "sort stops at max depth level",
			tasksResources: resource_info.NewResource(0, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "region1",
					Level: "region",
					Children: []*DomainInfo{
						{
							ID:                       "zone2",
							Level:                    "zone",
							AllocatablePods:          10,
							IdleOrReleasingResources: resource_info.NewResource(0, 0, 10),
							Children: []*DomainInfo{
								{ID: "rack4", Level: "rack", AllocatablePods: 3, IdleOrReleasingResources: resource_info.NewResource(0, 0, 3)},
								{ID: "rack3", Level: "rack", AllocatablePods: 7, IdleOrReleasingResources: resource_info.NewResource(0, 0, 7)},
							},
						},
						{
							ID:                       "zone1",
							Level:                    "zone",
							AllocatablePods:          5,
							IdleOrReleasingResources: resource_info.NewResource(0, 0, 5),
							Children: []*DomainInfo{
								{ID: "rack2", Level: "rack", AllocatablePods: 1, IdleOrReleasingResources: resource_info.NewResource(0, 0, 1)},
								{ID: "rack1", Level: "rack", AllocatablePods: 4, IdleOrReleasingResources: resource_info.NewResource(0, 0, 4)},
							},
						},
					},
				}
			},
			maxDepthLevel: "zone",
			expectedOrder: []DomainID{"zone1", "zone2"},
			checkLevel:    "region",
		},
		{
			name:           "sort nested hierarchy to rack level",
			tasksResources: resource_info.NewResource(0, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "region1",
					Level: "region",
					Children: []*DomainInfo{
						{
							ID:                       "zone1",
							Level:                    "zone",
							AllocatablePods:          15,
							IdleOrReleasingResources: resource_info.NewResource(0, 0, 15),
							Children: []*DomainInfo{
								{ID: "rack2", Level: "rack", AllocatablePods: 10, IdleOrReleasingResources: resource_info.NewResource(0, 0, 10)},
								{ID: "rack1", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
							},
						},
						{
							ID:                       "zone2",
							Level:                    "zone",
							AllocatablePods:          8,
							IdleOrReleasingResources: resource_info.NewResource(0, 0, 8),
							Children: []*DomainInfo{
								{ID: "rack4", Level: "rack", AllocatablePods: 3, IdleOrReleasingResources: resource_info.NewResource(0, 0, 3)},
								{ID: "rack3", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
							},
						},
					},
				}
			},
			maxDepthLevel: "rack",
			expectedOrder: []DomainID{"zone2", "zone1"}, // Zones sorted
			checkLevel:    "region",
		},
		{
			name:           "stable sort - same allocatable pods maintain order",
			tasksResources: resource_info.NewResource(0, 0, 1),
			setupTree: func() *DomainInfo {
				return &DomainInfo{
					ID:    "zone1",
					Level: "zone",
					Children: []*DomainInfo{
						{ID: "rack1", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
						{ID: "rack2", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
						{ID: "rack3", Level: "rack", AllocatablePods: 5, IdleOrReleasingResources: resource_info.NewResource(0, 0, 5)},
					},
				}
			},
			maxDepthLevel: "rack",
			expectedOrder: []DomainID{"rack1", "rack2", "rack3"},
			checkLevel:    "zone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tt.setupTree()
			sortTree(tt.tasksResources, root, tt.maxDepthLevel)

			if tt.expectedOrder == nil {
				return
			}

			// Verify the order
			if tt.checkLevel == "zone" && root != nil {
				require.Equal(t, len(tt.expectedOrder), len(root.Children))
				for i, child := range root.Children {
					assert.Equal(t, tt.expectedOrder[i], child.ID, "Child at position %d has wrong ID", i)
				}
			} else if tt.checkLevel == "region" && root != nil {
				require.Equal(t, len(tt.expectedOrder), len(root.Children))
				for i, child := range root.Children {
					assert.Equal(t, tt.expectedOrder[i], child.ID, "Child at position %d has wrong ID", i)
				}
			}
		})
	}
}

func TestSortTree_NestedSorting(t *testing.T) {
	// Test that both parent and child levels are sorted when maxDepth is deep enough
	tasksResources := resource_info.NewResource(0, 0, 1)
	root := &DomainInfo{
		ID:    "region1",
		Level: "region",
		Children: []*DomainInfo{
			{
				ID:                       "zone2",
				Level:                    "zone",
				AllocatablePods:          10,
				IdleOrReleasingResources: resource_info.NewResource(0, 0, 10),
				Children: []*DomainInfo{
					{ID: "rack4", Level: "rack", AllocatablePods: 7, IdleOrReleasingResources: resource_info.NewResource(0, 0, 7)},
					{ID: "rack3", Level: "rack", AllocatablePods: 3, IdleOrReleasingResources: resource_info.NewResource(0, 0, 3)},
				},
			},
			{
				ID:                       "zone1",
				Level:                    "zone",
				AllocatablePods:          5,
				IdleOrReleasingResources: resource_info.NewResource(0, 0, 5),
				Children: []*DomainInfo{
					{ID: "rack2", Level: "rack", AllocatablePods: 4, IdleOrReleasingResources: resource_info.NewResource(0, 0, 4)},
					{ID: "rack1", Level: "rack", AllocatablePods: 1, IdleOrReleasingResources: resource_info.NewResource(0, 0, 1)},
				},
			},
		},
	}

	sortTree(tasksResources, root, "rack")

	// Check regions are sorted
	require.Equal(t, 2, len(root.Children))
	assert.Equal(t, DomainID("zone1"), root.Children[0].ID)
	assert.Equal(t, DomainID("zone2"), root.Children[1].ID)

	// Check racks within zone1 are sorted
	require.Equal(t, 2, len(root.Children[0].Children))
	assert.Equal(t, DomainID("rack1"), root.Children[0].Children[0].ID)
	assert.Equal(t, DomainID("rack2"), root.Children[0].Children[1].ID)

	// Check racks within zone2 are sorted
	require.Equal(t, 2, len(root.Children[1].Children))
	assert.Equal(t, DomainID("rack3"), root.Children[1].Children[0].ID)
	assert.Equal(t, DomainID("rack4"), root.Children[1].Children[1].ID)
}
