// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"slices"
	"sort"
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

var testVectorMap = resource_info.NewResourceVectorMap()

func TestTopologyPlugin_subsetNodesFn(t *testing.T) {
	tests := []struct {
		name               string
		job                *jobs_fake.TestJobBasic
		allocatedPodGroups []*jobs_fake.TestJobBasic
		nodes              map[string]nodes_fake.TestNodeBasic
		nodesToDomains     map[string]DomainID
		setupTopologyTree  func() *Info

		domainParent             map[DomainID]DomainID
		domainLevel              map[DomainID]DomainLevel
		expectedError            string
		expectedJobFitError      string
		expectedNodesFirstSubset map[string]bool
	}{
		{
			name: "successful topology allocation - right nodes",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology:       "test-topology",
						RequiredLevel:  "zone",
						PreferredLevel: "rack",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"zone": "zone1",
						"rack": "rack1",
					},
				},
				"node-2": {
					CPUMillis:  400,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"zone": "zone1",
						"rack": "rack2",
					},
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"rack": {
							"rack1.zone1": {
								ID:                    "rack1.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
							"rack2.zone1": {
								ID:                    "rack2.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = []*DomainInfo{
					tree.DomainsByLevel["rack"]["rack1.zone1"],
					tree.DomainsByLevel["rack"]["rack2.zone1"],
				}

				return tree
			},
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedError: "",
			expectedNodesFirstSubset: map[string]bool{
				"node-1": true,
			},
		},
		{
			name: "successful topology allocation - required equal preferred",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology:       "test-topology",
						RequiredLevel:  "rack",
						PreferredLevel: "rack",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"zone": "zone1",
						"rack": "rack1",
					},
				},
				"node-2": {
					CPUMillis:  400,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"zone": "zone1",
						"rack": "rack2",
					},
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"rack": {
							"rack1.zone1": {
								ID:                    "rack1.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
							"rack2.zone1": {
								ID:                    "rack2.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = []*DomainInfo{
					tree.DomainsByLevel["rack"]["rack1.zone1"],
					tree.DomainsByLevel["rack"]["rack2.zone1"],
				}

				return tree
			},
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedError: "",
			expectedNodesFirstSubset: map[string]bool{
				"node-1": true,
			},
		},
		{
			name: "no topology constraint - early return",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					nil,
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			setupTopologyTree: func() *Info {
				return &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
				}
			},
			expectedError: "",
		},
		{
			name: "topology not found - error",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				Namespace:           "test-namespace",
				RequiredCPUsPerTask: 500,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology: "nonexistent-topology",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			setupTopologyTree: func() *Info {
				return &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
				}
			},
			expectedJobFitError: "Requested topology nonexistent-topology does not exist",
		},
		{
			name: "insufficient allocatable pods - no domains found",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 2000, // Too much for any node
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology:      "test-topology",
						RequiredLevel: "zone",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"zone": "zone1",
					},
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				return tree
			},
			expectedJobFitError: "topology test-topology, requirement zone couldn't be satisfied for job </test-job>: not enough resources in zone1 to allocate the job",
		},
		{
			name: "successful allocation with mixed GPU tasks - usePodCountAccounting returns false",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 2000,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology:       "test-topology",
						RequiredLevel:  "zone",
						PreferredLevel: "rack",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending, RequiredGPUs: ptr.To(int64(1))}, // Task with GPU
					{State: pod_status.Pending, RequiredGPUs: ptr.To(int64(0))}, // Task without GPU
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  2000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"rack": "rack1",
						"zone": "zone1",
					},
				},
				"node-2": {
					CPUMillis:  2000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
					Labels: map[string]string{
						"rack": "rack2",
						"zone": "zone1",
					},
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"rack": {
							"rack1.zone1": {
								ID:                    "rack1.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
								AllocatablePods:       allocatablePodsNotSet,
							},
							"rack2.zone1": {
								ID:                    "rack2.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
								AllocatablePods:       allocatablePodsNotSet,
							},
						},
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
								AllocatablePods:       allocatablePodsNotSet,
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = []*DomainInfo{
					tree.DomainsByLevel["rack"]["rack1.zone1"],
					tree.DomainsByLevel["rack"]["rack2.zone1"],
				}

				return tree
			},
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedError: "",
			expectedNodesFirstSubset: map[string]bool{
				"node-1": true,
				"node-2": true,
			},
		},
		{
			name: "getJobAllocatableDomains constrain definition error",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{
						Topology:       "test-topology",
						RequiredLevel:  "nonexistent-level",
						PreferredLevel: "rack",
					},
				),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name:      "test-topology",
					VectorMap: resource_info.NewResourceVectorMap(),
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"rack": {
							"rack1.zone1": {
								ID:                    "rack1.zone1",
								Level:                 "rack",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = []*DomainInfo{
					tree.DomainsByLevel["rack"]["rack1.zone1"],
				}

				return tree
			},
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedError: "topology constraint error: sub-group  specified 'nonexistent-level' as the required topology constraint level, but the topology tree 'test-topology' does not contain a level with this name",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("test %d: %s", i, tt.name)
			// Setup test data
			jobName := tt.job.Name
			clusterPodGroups := append(tt.allocatedPodGroups, tt.job)
			vectorMap := resource_info.NewResourceVectorMap()
			jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(clusterPodGroups, vectorMap)
			nodesInfoMap := nodes_fake.BuildNodesInfoMap(tt.nodes, tasksToNodeMap, nil, vectorMap)
			job := jobsInfoMap[common_info.PodGroupID(jobName)]

			// Setup topology tree
			topologyTree := tt.setupTopologyTree()
			if tt.nodesToDomains != nil {
				for nodeName, domainId := range tt.nodesToDomains {
					nodeInfo := nodesInfoMap[nodeName]
					leafLevel := len(topologyTree.TopologyResource.Spec.Levels) - 1
					domain := topologyTree.DomainsByLevel[DomainLevel(topologyTree.TopologyResource.Spec.Levels[leafLevel].NodeLabel)][domainId]
					for domain != nil {
						domain.AddNode(nodeInfo)
						parentDomainId := tt.domainParent[domain.ID]
						parentDomainLevel := tt.domainLevel[parentDomainId]
						domain = topologyTree.DomainsByLevel[parentDomainLevel][parentDomainId]
					}
				}
			}

			// Setup plugin
			plugin := &topologyPlugin{
				TopologyTrees: map[string]*Info{
					"test-topology": topologyTree,
				},
				subGroupNodeScores: map[subgroupName]map[string]float64{},
			}

			// Call the function under test
			subsets, err := plugin.subSetNodesFn(job, &job.RootSubGroupSet.SubGroupInfo,
				job.RootSubGroupSet.GetAllPodSets(), podgroup_info.GetTasksToAllocate(job, nil, nil, true),
				maps.Values(nodesInfoMap))

			// Check error
			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error '%s', but got nil", tt.expectedError)
					return
				}
				if err.Error() != tt.expectedError {
					t.Errorf("expected error '%s', but got '%s'", tt.expectedError, err.Error())
				}
				return
			}

			if tt.expectedJobFitError != "" {
				if len(job.JobFitErrors) == 0 {
					t.Errorf("expected job fit error '%s', but got nil", tt.expectedJobFitError)
				}
				if job.JobFitErrors[0].Messages()[0] != tt.expectedJobFitError {
					t.Errorf("expected job fit error '%s', but got '%s'", tt.expectedJobFitError, job.JobFitErrors[0].Messages()[0])
				}
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Verify nodes
			if tt.expectedNodesFirstSubset != nil {
				if len(subsets) == 0 {
					t.Errorf("expected subsets to be filled with %v, but got nil", tt.expectedNodesFirstSubset)
				}
				if len(subsets[0]) != len(tt.expectedNodesFirstSubset) {
					t.Errorf("expected subsets to be filled with %v, but got %v", tt.expectedNodesFirstSubset, subsets[0])
				}
				for _, node := range subsets[0] {
					_, ok := tt.expectedNodesFirstSubset[node.Name]
					assert.True(t, ok, "expected node %s to be in subset %v", node.Name, tt.expectedNodesFirstSubset)
				}
			}
		})
	}
}

func TestTopologyPlugin_calculateRelevantDomainLevels(t *testing.T) {
	tests := []struct {
		name           string
		subGroupSet    *subgroup_info.SubGroupSet
		topologyTree   *Info
		expectedLevels []DomainLevel
		expectedError  string
	}{
		{
			name: "both required and preferred placement specified",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					RequiredLevel:  "zone",
					PreferredLevel: "rack",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"rack",
				"zone",
			},
			expectedError: "",
		},
		{
			name: "only required placement specified",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:      "test-topology",
					RequiredLevel: "zone",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"zone",
			},
			expectedError: "",
		},
		{
			name: "only preferred placement specified",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					PreferredLevel: "rack",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"rack",
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "no placement annotations specified",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology: "test-topology",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "no topology constraints were found for subgroup test-subgroup, with topology name test-topology",
		},
		{
			name: "required placement not found in topology",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:      "test-topology",
					RequiredLevel: "nonexistent",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "topology constraint error: sub-group test-subgroup specified 'nonexistent' as the required topology constraint level, but the topology tree 'test-topology' does not contain a level with this name",
		},
		{
			name: "preferred placement not found in topology",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					PreferredLevel: "nonexistent",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "topology constraint error: sub-group test-subgroup specified 'nonexistent' as the preferred topology constraint level, but the topology tree 'test-topology' does not contain a level with this name",
		},
		{
			name: "required placement at first level",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:      "test-topology",
					RequiredLevel: "rack",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"rack",
			},
			expectedError: "",
		},
		{
			name: "preferred placement at first level",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					PreferredLevel: "rack",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"rack",
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "preferred placement at middle level",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					PreferredLevel: "zone",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "single level topology with preferred placement",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					PreferredLevel: "zone",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"zone",
				"root",
			},
			expectedError: "",
		},
		{
			name: "complex topology with multiple levels",
			subGroupSet: subgroup_info.NewSubGroupSet("test-subgroup",
				&topology_info.TopologyConstraintInfo{
					Topology:       "test-topology",
					RequiredLevel:  "region",
					PreferredLevel: "zone",
				},
			),
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []DomainLevel{
				"zone",
				"region",
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &topologyPlugin{}

			result, err := plugin.calculateRelevantDomainLevels(&tt.subGroupSet.SubGroupInfo, tt.topologyTree)

			// Check error
			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error '%s', but got nil", tt.expectedError)
					return
				}
				if err.Error() != tt.expectedError {
					t.Errorf("expected error '%s', but got '%s'", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check result
			if tt.expectedLevels == nil {
				if result != nil {
					t.Errorf("expected nil result, but got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected result %v, but got nil", tt.expectedLevels)
				return
			}

			// Compare maps
			if len(result) != len(tt.expectedLevels) {
				t.Errorf("expected %d levels, but got %d", len(tt.expectedLevels), len(result))
			}

			if !slices.Equal(result, tt.expectedLevels) {
				t.Errorf("expected %v, but got %v", tt.expectedLevels, result)
			}
		})
	}
}

func TestTopologyPlugin_calcTreeAllocatable(t *testing.T) {
	twoRacksOneZoneTree := func() *Info {
		tree := &Info{
			Name: "test-topology",
			TopologyResource: &kaiv1alpha1.Topology{
				Spec: kaiv1alpha1.TopologySpec{
					Levels: []kaiv1alpha1.TopologyLevel{
						{NodeLabel: "zone"},
						{NodeLabel: "rack"},
					},
				},
			},
			VectorMap: testVectorMap,
			DomainsByLevel: map[DomainLevel]LevelDomainInfos{
				"rack": {
					"rack1.zone1": {
						ID:                    "rack1.zone1",
						Level:                 "rack",
						Nodes:                 map[string]*node_info.NodeInfo{},
						IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
					},
					"rack2.zone1": {
						ID:                    "rack2.zone1",
						Level:                 "rack",
						Nodes:                 map[string]*node_info.NodeInfo{},
						IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
					},
				},
				"zone": {
					"zone1": {
						ID:                    "zone1",
						Level:                 "zone",
						Nodes:                 map[string]*node_info.NodeInfo{},
						IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
					},
				},
			},
		}

		tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
			rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
		}

		// Set parent relationships
		tree.DomainsByLevel["zone"]["zone1"].Children = []*DomainInfo{
			tree.DomainsByLevel["rack"]["rack1.zone1"],
			tree.DomainsByLevel["rack"]["rack2.zone1"],
		}

		return tree
	}

	tests := []struct {
		name                       string
		job                        *jobs_fake.TestJobBasic
		allocatedPodGroups         []*jobs_fake.TestJobBasic
		nodes                      map[string]nodes_fake.TestNodeBasic
		nodesToDomains             map[string]DomainID
		setupTopologyTree          func() *Info
		domainParent               map[DomainID]DomainID
		domainLevel                map[DomainID]DomainLevel
		expectedMaxAllocatablePods int
		expectedDomains            map[DomainID]*DomainInfo
	}{
		{
			name: "two level topology - parent takes child values when children can allocate full job",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 4,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 4,
				},
			},
		},
		{
			name: "children cannot allocate full job individually - parent sums allocations",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 800, // Each node can only fit 1 pod (1000/800 = 1)
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 2,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 1, // Can only fit 1 pod
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 1, // Can only fit 1 pod
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 2, // Sum of children allocations: 1 + 1
				},
			},
		},
		{
			name: "mixed distances - parent takes minimum distance",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  500,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  500,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-3": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack1.zone1",
				"node-3": "rack2.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 4,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 4,
				},
			},
		},
		{
			name: "no leaf domains - no allocatable domains",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 2000, // Too much for any node
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "zone1",
			},
			setupTopologyTree: func() *Info {
				tree := &Info{
					Name: "test-topology",
					TopologyResource: &kaiv1alpha1.Topology{
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
					DomainsByLevel: map[DomainLevel]LevelDomainInfos{
						"zone": {
							"zone1": {
								ID:                    "zone1",
								Level:                 "zone",
								Nodes:                 map[string]*node_info.NodeInfo{},
								IdleOrReleasingVector: resource_info.NewResource(0, 0, 0).ToVector(testVectorMap),
							},
						},
					},
				}

				tree.DomainsByLevel[rootLevel] = map[DomainID]*DomainInfo{
					rootDomainId: tree.DomainsByLevel["zone"]["zone1"],
				}

				return tree
			},
			expectedMaxAllocatablePods: 0,
			expectedDomains:            map[DomainID]*DomainInfo{
				// No domains should have allocations since no nodes can accommodate the job
			},
		},
		{
			name: "Can pipeline on domain with releasing pods",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			allocatedPodGroups: []*jobs_fake.TestJobBasic{
				{
					Name:                "running-job",
					RequiredCPUsPerTask: 500,
					Tasks: []*tasks_fake.TestTaskBasic{
						{
							State:    pod_status.Releasing,
							NodeName: "node-1",
						},
						{
							State:    pod_status.Releasing,
							NodeName: "node-2",
						},
					},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 4,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 4,
				},
			},
		},
		{
			name: "Job requests 0 resources - set maximal amount of pods on each node",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 0,
				IsBestEffortJob:     true, // ensures tasks are generated with no requested resources
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 8,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 4,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 4,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 8,
				},
			},
		},
		{
			name: "fractional GPU - all devices partially allocated, remaining capacity accommodates pods",
			job: &jobs_fake.TestJobBasic{
				Name:                "pending-job",
				RequiredGPUsPerTask: 0.4,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			allocatedPodGroups: []*jobs_fake.TestJobBasic{
				{
					Name:                "running-job",
					RequiredGPUsPerTask: 0.5,
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Running, NodeName: "node-1", GPUGroups: []string{"GPU-0"}},
						{State: pod_status.Running, NodeName: "node-1", GPUGroups: []string{"GPU-1"}},
					},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					GPUs:       2,
					GPUMemory:  1000,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 2,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 0,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 2,
				},
			},
		},
		{
			name: "fractional GPU - single partially-used device with remaining capacity",
			job: &jobs_fake.TestJobBasic{
				Name:                "pending-job",
				RequiredGPUsPerTask: 0.4,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			allocatedPodGroups: []*jobs_fake.TestJobBasic{
				{
					Name:                "running-job",
					RequiredGPUsPerTask: 0.5,
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Running, NodeName: "node-1", GPUGroups: []string{"GPU-0"}},
					},
				},
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					GPUs:       1,
					GPUMemory:  1000,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]DomainID{
				"node-1": "rack1.zone1",
			},
			setupTopologyTree: twoRacksOneZoneTree,
			domainParent: map[DomainID]DomainID{
				"rack1.zone1": "zone1",
				"rack2.zone1": "zone1",
			},
			domainLevel: map[DomainID]DomainLevel{
				"zone1": "zone",
			},
			expectedMaxAllocatablePods: 1,
			expectedDomains: map[DomainID]*DomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 1,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Level:           "rack",
					AllocatablePods: 0,
				},
				"zone1": {
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobName := tt.job.Name
			clusterPodGroups := append(tt.allocatedPodGroups, tt.job)
			vectorMap := resource_info.NewResourceVectorMap()
			jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(clusterPodGroups, vectorMap)
			nodesInfoMap := nodes_fake.BuildNodesInfoMap(tt.nodes, tasksToNodeMap, nil, vectorMap)
			job := jobsInfoMap[common_info.PodGroupID(jobName)]

			topologyTree := tt.setupTopologyTree()
			for nodeName, domainId := range tt.nodesToDomains {
				nodeInfo := nodesInfoMap[nodeName]
				leafLevel := len(topologyTree.TopologyResource.Spec.Levels) - 1
				domain := topologyTree.DomainsByLevel[DomainLevel(topologyTree.TopologyResource.Spec.Levels[leafLevel].NodeLabel)][domainId]
				for domain != nil {
					domain.AddNode(nodeInfo)
					parentDomainId := tt.domainParent[domain.ID]
					parentDomainLevel := tt.domainLevel[parentDomainId]
					domain = topologyTree.DomainsByLevel[parentDomainLevel][parentDomainId]
				}
			}

			plugin := &topologyPlugin{}

			// Call the function under test
			err := plugin.calcTreeAllocatable(podgroup_info.GetTasksToAllocate(job, nil, nil, true), topologyTree.DomainsByLevel[rootLevel][rootDomainId])
			if err != nil {
				t.Errorf("failed to calc tree allocatable. job: %s, error: %v", job.PodGroup.Name, err)
			}
			maxAllocatablePods := topologyTree.DomainsByLevel[rootLevel][rootDomainId].AllocatablePods

			// Assert
			if maxAllocatablePods != tt.expectedMaxAllocatablePods {
				t.Errorf("expected max allocatable pods %d, got %d", tt.expectedMaxAllocatablePods, maxAllocatablePods)
			}

			if len(tt.expectedDomains) == 0 {
				// Check that no domains have allocations
				for _, levelDomains := range topologyTree.DomainsByLevel {
					for _, domain := range levelDomains {
						if domain.AllocatablePods != 0 {
							t.Errorf("expected domain %s to have 0 AllocatablePods, got %d",
								domain.ID, domain.AllocatablePods)
						}
					}
				}
				return
			}

			for domainID, expectedDomain := range tt.expectedDomains {
				actualDomain, exists := topologyTree.DomainsByLevel[expectedDomain.Level][domainID]
				if !exists {
					t.Errorf("expected domain %s not found", domainID)
					continue
				}
				if actualDomain.AllocatablePods != expectedDomain.AllocatablePods {
					t.Errorf("domain %s: expected AllocatablePods %d, got %d",
						domainID, expectedDomain.AllocatablePods, actualDomain.AllocatablePods)
				}
			}
		})
	}
}

func TestTopologyPlugin_getJobAllocatableDomains(t *testing.T) {
	tests := []struct {
		name               string
		job                *podgroup_info.PodGroupInfo
		topologyConstraint *topology_info.TopologyConstraintInfo
		topologyTree       *Info
		taskOrderFunc      common_info.LessFn
		expectedDomains    []*DomainInfo
		expectedError      string
		expectedFitErrors  []common_info.JobFitError
	}{
		{
			name: "return multi domain",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel:  "zone",
				PreferredLevel: "rack",
			},
			topologyTree: &Info{
				Name: "test-topology",
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"rack": {
						"rack1.zone1": {
							ID:              "rack1.zone1",
							Level:           "rack",
							AllocatablePods: 2,
						},
						"rack2.zone1": {
							ID:              "rack2.zone1",
							Level:           "rack",
							AllocatablePods: 1,
						},
					},
					"zone": {
						"zone1": {
							ID:              "zone1",
							Level:           "zone",
							AllocatablePods: 3,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{
				{
					ID:              "rack1.zone1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				{
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 3,
				},
			},
			expectedError: "",
		},
		{
			name: "no domains can allocate the job",
			job: &podgroup_info.PodGroupInfo{
				Name:      "test-job",
				Namespace: "test-namespace",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel: "zone",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"rack": {
						"rack1.zone1": {
							ID:              "rack1.zone1",
							Level:           "rack",
							AllocatablePods: 1, // Can only fit 1 pod, job needs 2
						},
					},
					"zone": {
						"zone1": {
							ID:              "zone1",
							Level:           "zone",
							AllocatablePods: 1, // Can only fit 1 pod, job needs 2
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{},
			expectedFitErrors: []common_info.JobFitError{
				common_info.NewTopologyFitError(
					"test-job",
					"test",
					"test-namespace",
					"zone1",
					common_info.UnschedulableWorkloadReason,
					[]string{"node-group zone1 can allocate only 1 of 2 required pods"},
				),
				common_info.NewJobFitError(
					"test-job",
					"test",
					"test-namespace",
					common_info.UnschedulableWorkloadReason,
					[]string{"topology test-topology, requirement zone couldn't be satisfied for job <test-namespace/test-job>, subgroup test"}),
			},
		},
		{
			name: "no domains can allocate the job - using IdleOrReleasingResources",
			job: &podgroup_info.PodGroupInfo{
				Name:      "test-job",
				Namespace: "test-namespace",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending, ResReq: resource_info.NewResourceRequirementsWithGpus(1)},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending, ResReq: resource_info.NewResourceRequirements(0, 1000, 0)},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel: "zone",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"rack": {
						"rack1.zone1": {
							ID:                    "rack1.zone1",
							Level:                 "rack",
							IdleOrReleasingVector: resource_info.NewResource(500, 0, 0).ToVector(testVectorMap), // Insufficient resources
							AllocatablePods:       -1,
						},
						"rack2.zone2": {
							ID:                    "rack2.zone2",
							Level:                 "rack",
							IdleOrReleasingVector: resource_info.NewResource(600, 0, 0).ToVector(testVectorMap), // Insufficient resources
							AllocatablePods:       -1,
						},
					},
					"zone": {
						"zone1": {
							ID:                    "zone1",
							Level:                 "zone",
							IdleOrReleasingVector: resource_info.NewResource(500, 0, 0).ToVector(testVectorMap), // Insufficient resources
							AllocatablePods:       -1,
						},
						"zone2": {
							ID:                    "zone2",
							Level:                 "zone",
							IdleOrReleasingVector: resource_info.NewResource(600, 0, 0).ToVector(testVectorMap), // Insufficient resources
							AllocatablePods:       -1,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{},
			expectedFitErrors: []common_info.JobFitError{
				common_info.NewTopologyFitError(
					"test-job",
					"test",
					"test-namespace",
					"zone1",
					common_info.UnschedulableWorkloadReason,
					[]string{
						"node-group(s) didn't have enough resources: GPUs",
						"node-group(s) didn't have enough resources: CPU cores",
					},
					"zone1 didn't have enough resource: GPUs, requested: 1, available: 0",
					"zone1 didn't have enough resources: CPU cores, requested: 1, available: 0.5",
				),
				common_info.NewTopologyFitError(
					"test-job",
					"test",
					"test-namespace",
					"zone2",
					common_info.UnschedulableWorkloadReason,
					[]string{
						"node-group(s) didn't have enough resources: GPUs",
						"node-group(s) didn't have enough resources: CPU cores",
					},
					"zone2 didn't have enough resource: GPUs, requested: 1, available: 0",
					"zone2 didn't have enough resources: CPU cores, requested: 1, available: 0.6",
				),
				common_info.NewJobFitError(
					"test-job",
					"test",
					"test-namespace",
					common_info.UnschedulableWorkloadReason,
					[]string{"topology test-topology, requirement zone couldn't be satisfied for job <test-namespace/test-job>, subgroup test"},
				),
			},
		},
		{
			name: "no relevant domain levels",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 1, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel:  "zone",
				PreferredLevel: "rack",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"datacenter": {
						"datacenter1": {
							ID:              "datacenter1",
							Level:           "datacenter",
							AllocatablePods: 1,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: nil,
			expectedError:   "topology constraint error: sub-group test specified 'zone' as the required topology constraint level, but the topology tree 'test-topology' does not contain a level with this name",
		},
		{
			name: "complex topology with multiple levels",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 3, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
							"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel:  "region",
				PreferredLevel: "zone",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"rack": {
						"rack1.zone1.region1": {
							ID:              "rack1.zone1.region1",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack2.zone1.region1": {
							ID:              "rack2.zone1.region1",
							Level:           "rack",
							AllocatablePods: 3,
						},
					},
					"zone": {
						"zone1.region1": {
							ID:              "zone1.region1",
							Level:           "zone",
							AllocatablePods: 6,
						},
					},
					"region": {
						"region1": {
							ID:              "region1",
							Level:           "region",
							AllocatablePods: 9,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{
				{
					ID:              "zone1.region1",
					Level:           "zone",
					AllocatablePods: 6,
				},
				{
					ID:              "region1",
					Level:           "region",
					AllocatablePods: 9,
				},
			},
			expectedError: "",
		},
		{
			name: "mixed task statuses - some pending, some running",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Running, NodeName: "node1"},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
							"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel: "zone",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"zone": {
						"zone1": {
							ID:    "zone1",
							Level: "zone",
							Nodes: map[string]*node_info.NodeInfo{
								"node1": {
									Node: &v1.Node{
										ObjectMeta: metav1.ObjectMeta{
											Name: "node1",
										},
									},
								},
							},
							AllocatablePods: 2,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{
				{
					ID:              "zone1",
					Level:           "zone",
					AllocatablePods: 2,
				},
			},
			expectedError: "",
		},
		{
			name: "mixed task statuses with required constraint - choose zone with existing pods",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 2, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Running, NodeName: "node2"},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
							"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel: "zone",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"zone": {
						"zone1": {
							ID:    "zone1",
							Level: "zone",
							Nodes: map[string]*node_info.NodeInfo{
								"node1": {
									Node: &v1.Node{
										ObjectMeta: metav1.ObjectMeta{
											Name: "node1",
										},
									},
								},
							},
							AllocatablePods: 2,
						},
						"zone2": {
							ID:    "zone2",
							Level: "zone",
							Nodes: map[string]*node_info.NodeInfo{
								"node2": {
									Node: &v1.Node{
										ObjectMeta: metav1.ObjectMeta{
											Name: "node2",
										},
									},
								},
							},
							AllocatablePods: 2,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{
				{
					ID:              "zone2",
					Level:           "zone",
					AllocatablePods: 2,
				},
			},
			expectedError: "",
		},
		{
			name: "Return multiple levels of domains",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodSets: map[string]*subgroup_info.PodSet{
					podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, 4, nil).
						WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
							"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
							"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
							"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
							"pod4": {UID: "pod4", Name: "pod4", Status: pod_status.Pending},
						}),
				},
			},
			topologyConstraint: &topology_info.TopologyConstraintInfo{
				RequiredLevel:  "region",
				PreferredLevel: "rack",
			},
			topologyTree: &Info{
				Name:      "test-topology",
				VectorMap: resource_info.NewResourceVectorMap(),
				TopologyResource: &kaiv1alpha1.Topology{
					Spec: kaiv1alpha1.TopologySpec{
						Levels: []kaiv1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[DomainLevel]LevelDomainInfos{
					"rack": {
						"rack1.zone1.region1": {
							ID:              "rack1.zone1.region1",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack2.zone1.region1": {
							ID:              "rack2.zone1.region1",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack1.zone2.region1": {
							ID:              "rack1.zone2.region1",
							Level:           "rack",
							AllocatablePods: 2,
						},
						"rack2.zone2.region1": {
							ID:              "rack2.zone1.region1",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack3.zone3.region1": {
							ID:              "rack3.zone2.region1",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack4.zone2.region1": {
							ID:              "rack4.zone2.region1",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack5.zone2.region1": {
							ID:              "rack5.zone2.region1",
							Level:           "rack",
							AllocatablePods: 1,
						},
					},
					"zone": {
						"zone1.region1": {
							ID:              "zone1.region1",
							Level:           "zone",
							AllocatablePods: 6,
							Children: []*DomainInfo{
								{
									ID:              "rack1.zone1.region1",
									Level:           "rack",
									AllocatablePods: 3,
								},
								{
									ID:              "rack2.zone1.region1",
									Level:           "rack",
									AllocatablePods: 3,
								},
							},
						},
						"zone2.region1": {
							ID:              "zone2.region1",
							Level:           "zone",
							AllocatablePods: 6,
							Children: []*DomainInfo{
								{
									ID:              "rack1.zone2.region1",
									Level:           "rack",
									AllocatablePods: 2,
								},
								{
									ID:              "rack2.zone1.region1",
									Level:           "rack",
									AllocatablePods: 1,
								},
								{
									ID:              "rack3.zone2.region1",
									Level:           "rack",
									AllocatablePods: 1,
								},
								{
									ID:              "rack4.zone2.region1",
									Level:           "rack",
									AllocatablePods: 1,
								},
								{
									ID:              "rack5.zone2.region1",
									Level:           "rack",
									AllocatablePods: 1,
								},
							},
						},
					},
					"region": {
						"region1": {
							ID:              "region1",
							Level:           "region",
							AllocatablePods: 9,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*DomainInfo{
				{
					ID:              "zone1.region1",
					Level:           "zone",
					AllocatablePods: 6,
				},
				{
					ID:              "zone2.region1",
					Level:           "zone",
					AllocatablePods: 6,
				},
				{
					ID:              "region1",
					Level:           "region",
					AllocatablePods: 9,
				},
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &topologyPlugin{}

			tt.job.RootSubGroupSet = subgroup_info.NewSubGroupSet("test", tt.topologyConstraint)
			for _, podSet := range tt.job.PodSets {
				tt.job.RootSubGroupSet.AddPodSet(podSet)
			}

			tasks := podgroup_info.GetTasksToAllocate(tt.job, nil, nil, true)
			tasksResources := resource_info.NewResource(0, 0, 0)
			for _, task := range tasks {
				tasksResources.AddResourceRequirements(task.ResReq)
			}
			tasksCount := len(tasks)

			result, err := plugin.getJobAllocatableDomains(tt.job, &tt.job.RootSubGroupSet.SubGroupInfo,
				tt.job.RootSubGroupSet.GetAllPodSets(), tasksResources.ToVector(testVectorMap), tasksCount,
				tt.topologyTree)

			// Check error
			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error '%s', but got nil", tt.expectedError)
					return
				}
				if err.Error() != tt.expectedError {
					t.Errorf("expected error '%s', but got '%s'", tt.expectedError, err.Error())
				}
				if len(tt.job.JobFitErrors) != len(tt.expectedFitErrors) {
					t.Errorf("expected %d fit errors, but got %d", len(tt.expectedFitErrors), len(tt.job.JobFitErrors))
				}
				if len(tt.expectedFitErrors) > 0 {
					for i, expectedFitError := range tt.expectedFitErrors {
						actualFitError := tt.job.JobFitErrors[i]
						if !slices.Equal(expectedFitError.Messages(), actualFitError.Messages()) {
							t.Errorf("expected fit error %d: messages %v, but got %v", i, expectedFitError.Messages(), actualFitError.Messages())
						}
						if expectedFitError.DetailedMessage() != actualFitError.DetailedMessage() {
							t.Errorf("expected fit error %d: detailed message %s, but got %s", i, expectedFitError.DetailedMessage(), actualFitError.DetailedMessage())
						}
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check result
			if len(result) != len(tt.expectedDomains) {
				t.Errorf("expected %d domains, but got %d", len(tt.expectedDomains), len(result))
				return
			}

			// Sort both slices by domain ID for consistent comparison
			sortDomains := func(domains []*DomainInfo) {
				sort.Slice(domains, func(i, j int) bool {
					return domains[i].ID < domains[j].ID
				})
			}
			sortDomains(result)
			sortDomains(tt.expectedDomains)

			for i, expectedDomain := range tt.expectedDomains {
				if i >= len(result) {
					t.Errorf("expected domain at index %d not found in result", i)
					continue
				}

				actualDomain := result[i]
				if actualDomain.ID != expectedDomain.ID {
					t.Errorf("domain %d: expected ID %s, got %s", i, expectedDomain.ID, actualDomain.ID)
				}
				if actualDomain.Level != expectedDomain.Level {
					t.Errorf("domain %d: expected Level %s, got %s", i, expectedDomain.Level, actualDomain.Level)
				}
				if actualDomain.AllocatablePods != expectedDomain.AllocatablePods {
					t.Errorf("domain %d: expected AllocatablePods %d, got %d", i, expectedDomain.AllocatablePods, actualDomain.AllocatablePods)
				}
			}
		})
	}
}
