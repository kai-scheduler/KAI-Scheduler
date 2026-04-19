// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"errors"
	"slices"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/utils/ptr"
	kueuev1alpha1 "sigs.k8s.io/kueue/apis/kueue/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// Mock session state provider for testing
type mockSessionStateProvider struct {
	states                   map[types.UID]*k8sframework.CycleState
	GetSessionStateCallCount int
}

func newMockSessionStateProvider() *mockSessionStateProvider {
	return &mockSessionStateProvider{
		states:                   make(map[types.UID]*k8sframework.CycleState),
		GetSessionStateCallCount: 0,
	}
}

func (m *mockSessionStateProvider) GetSessionStateForResource(uid types.UID) k8s_internal.SessionState {
	m.GetSessionStateCallCount++
	if state, exists := m.states[uid]; exists {
		return k8s_internal.SessionState(state)
	}
	state := k8sframework.NewCycleState()
	m.states[uid] = state
	return k8s_internal.SessionState(state)
}

func TestTopologyPlugin_prePredicateFn(t *testing.T) {
	tests := []struct {
		name                    string
		job                     *jobs_fake.TestJobBasic
		jobTopologyConstraint   *enginev2alpha2.TopologyConstraint
		allocatedPodGroups      []*jobs_fake.TestJobBasic
		nodes                   map[string]nodes_fake.TestNodeBasic
		nodesToDomains          map[string]TopologyDomainID
		setupTopologyTree       func() *TopologyInfo
		setupSessionState       func(provider *mockSessionStateProvider, jobUID types.UID)
		expectedError           string
		expectedCacheCalls      int
		expectedRelevantDomains []*TopologyDomainInfo
	}{
		{
			name: "successful topology allocation - cache right",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology:               "test-topology",
				RequiredTopologyLevel:  "zone",
				PreferredTopologyLevel: "rack",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
				"node-2": {
					CPUMillis:  400,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"rack": {
							"rack1.zone1": {
								ID:    "rack1.zone1",
								Name:  "rack1",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
							"rack2.zone1": {
								ID:    "rack2.zone1",
								Name:  "rack2",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = map[TopologyDomainID]*TopologyDomainInfo{
					"rack1.zone1": tree.DomainsByLevel["rack"]["rack1.zone1"],
					"rack2.zone1": tree.DomainsByLevel["rack"]["rack2.zone1"],
				}
				tree.DomainsByLevel["rack"]["rack1.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]
				tree.DomainsByLevel["rack"]["rack2.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedError:      "",
			expectedCacheCalls: 2,
			expectedRelevantDomains: []*TopologyDomainInfo{
				{
					ID:    "rack1.zone1",
					Name:  "rack1",
					Level: "rack",
				},
			},
		},
		{
			name: "no topology constraint - early return",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology: "test-topology",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			setupTopologyTree: func() *TopologyInfo {
				return &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
				}
			},
			expectedError:      "",
			expectedCacheCalls: 0,
		},
		{
			name: "topology not found - error",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology: "nonexistent-topology",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			setupTopologyTree: func() *TopologyInfo {
				return &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
				}
			},
			expectedError:      "matching topology tree haven't been found for job test-job, workload topology name: nonexistent-topology",
			expectedCacheCalls: 0,
		},
		{
			name: "cache already populated - early return",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology: "test-topology",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			setupTopologyTree: func() *TopologyInfo {
				return &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
				}
			},
			setupSessionState: func(provider *mockSessionStateProvider, jobUID types.UID) {
				state := provider.GetSessionStateForResource(jobUID)
				state.Write(
					ksf.StateKey(topologyPluginName),
					&topologyStateData{
						relevantDomains: []*TopologyDomainInfo{
							{
								ID:              "zone1",
								Name:            "zone1",
								Level:           "zone",
								AllocatablePods: 1,
							},
						},
					},
				)
				provider.GetSessionStateCallCount = 0 // Do not count this test data initialization as a call
			},
			expectedError:      "",
			expectedCacheCalls: 1, // read once and return, do not write
		},
		{
			name: "insufficient allocatable pods - no domains found",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 2000, // Too much for any node
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology: "test-topology",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedError:      "",
			expectedCacheCalls: 1,
		},
		{
			name: "getBestJobAllocatableDomains constrain definition error",
			job: &jobs_fake.TestJobBasic{
				Name:                "test-job",
				RequiredCPUsPerTask: 500,
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
				},
			},
			jobTopologyConstraint: &enginev2alpha2.TopologyConstraint{
				Topology:               "test-topology",
				RequiredTopologyLevel:  "nonexistent-level",
				PreferredTopologyLevel: "rack",
			},
			nodes: map[string]nodes_fake.TestNodeBasic{
				"node-1": {
					CPUMillis:  1000,
					GPUs:       6,
					MaxTaskNum: ptr.To(100),
				},
			},
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "rack1.zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"rack": {
							"rack1.zone1": {
								ID:    "rack1.zone1",
								Name:  "rack1",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = map[TopologyDomainID]*TopologyDomainInfo{
					"rack1.zone1": tree.DomainsByLevel["rack"]["rack1.zone1"],
				}
				tree.DomainsByLevel["rack"]["rack1.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedError:      "the topology test-topology doesn't have a level matching the required(nonexistent-level) specified for the job test-job",
			expectedCacheCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			jobName := tt.job.Name
			clusterPodGroups := append(tt.allocatedPodGroups, tt.job)
			jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(clusterPodGroups)
			nodesInfoMap := nodes_fake.BuildNodesInfoMap(tt.nodes, tasksToNodeMap)
			job := jobsInfoMap[common_info.PodGroupID(jobName)]

			// Setup topology tree
			topologyTree := tt.setupTopologyTree()
			if tt.nodesToDomains != nil {
				for nodeName, domainId := range tt.nodesToDomains {
					nodeInfo := nodesInfoMap[nodeName]
					leafLevel := len(topologyTree.TopologyResource.Spec.Levels) - 1
					domain := topologyTree.DomainsByLevel[topologyTree.TopologyResource.Spec.Levels[leafLevel].NodeLabel][domainId]
					for domain != nil {
						if nodeInfo.Node.Labels == nil {
							nodeInfo.Node.Labels = map[string]string{
								domain.Level: domain.Name,
							}
						} else {
							nodeInfo.Node.Labels[domain.Level] = domain.Name
						}
						domain.AddNode(nodeInfo)
						domain = domain.Parent
					}
				}
			}

			// Setup session state provider
			sessionStateProvider := newMockSessionStateProvider()
			if tt.setupSessionState != nil {
				tt.setupSessionState(sessionStateProvider, types.UID(job.PodGroupUID))
			}

			// Setup plugin
			plugin := &topologyPlugin{
				sessionStateGetter: sessionStateProvider,
				nodesInfos:         nodesInfoMap,
				TopologyTrees: map[string]*TopologyInfo{
					"test-topology": topologyTree,
				},
				taskOrderFunc: func(l, r interface{}) bool {
					return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
				},
				subGroupOrderFunc: func(l, r interface{}) bool {
					return l.(*podgroup_info.PodGroupInfo).Name < r.(*podgroup_info.PodGroupInfo).Name
				},
			}

			// Update job with topology constraints based on test case
			if tt.jobTopologyConstraint != nil {
				job.PodGroup.Spec.TopologyConstraint = *tt.jobTopologyConstraint
			}

			// Call the function under test
			err := plugin.prePredicateFn(nil, job)

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

			// Check cache write if expected
			if tt.expectedCacheCalls >= 2 { // >=2 because the first call is read, the second is write
				state := sessionStateProvider.GetSessionStateForResource(types.UID(job.PodGroupUID))
				stateData, err := (ksf.CycleState)(state).Read(ksf.StateKey(topologyPluginName))
				if err != nil {
					t.Errorf("failed to read state data: %v", err)
					return
				}

				topologyState := stateData.(*topologyStateData)
				if len(topologyState.relevantDomains) != len(tt.expectedRelevantDomains) {
					t.Errorf("expected %d relevant domains, got %d",
						len(tt.expectedRelevantDomains), len(topologyState.relevantDomains))
					return
				}

				for i, expectedDomain := range tt.expectedRelevantDomains {
					if i >= len(topologyState.relevantDomains) {
						t.Errorf("expected domain at index %d not found", i)
						continue
					}

					actualDomain := topologyState.relevantDomains[i]
					if actualDomain.ID != expectedDomain.ID {
						t.Errorf("domain %d: expected ID %s, got %s", i, expectedDomain.ID, actualDomain.ID)
					}
					if actualDomain.Level != expectedDomain.Level {
						t.Errorf("domain %d: expected Level %s, got %s", i, expectedDomain.Level, actualDomain.Level)
					}
				}
			} else {
				// Verify no cache write occurred
				testGetSessionStateCallCount := sessionStateProvider.GetSessionStateCallCount

				state := sessionStateProvider.GetSessionStateForResource(types.UID(job.PodGroupUID))
				_, err := (ksf.CycleState)(state).Read(ksf.StateKey(topologyPluginName))
				if err == nil && testGetSessionStateCallCount > 1 { // >1 because the first call is read, the second is write
					t.Errorf("expected no cache write, but found more than 1 sessionState call (the first is read, the second is write)")
				} else if !errors.Is(err, ksf.ErrNotFound) && tt.expectedCacheCalls == 0 {
					t.Errorf("expected ErrNotFound error, but got: %v", err)
				}
			}
		})
	}
}

func TestTopologyPlugin_calculateRelevantDomainLevels(t *testing.T) {
	tests := []struct {
		name            string
		job             *podgroup_info.PodGroupInfo
		jobTopologyName string
		topologyTree    *TopologyInfo
		expectedLevels  []string
		expectedError   string
	}{
		{
			name: "both required and preferred placement specified",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "zone",
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"rack",
				"zone",
			},
			expectedError: "",
		},
		{
			name: "only required placement specified",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "zone",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"rack",
				"zone",
			},
			expectedError: "",
		},
		{
			name: "only preferred placement specified",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"rack",
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "no placement annotations specified",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-job",
						Annotations: map[string]string{},
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "no topology placement annotations found for job test-job, workload topology name: test-topology",
		},
		{
			name: "required placement not found in topology",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "nonexistent",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "the topology test-topology doesn't have a level matching the required(nonexistent) specified for the job test-job",
		},
		{
			name: "preferred placement not found in topology",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							PreferredTopologyLevel: "nonexistent",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: nil,
			expectedError:  "the topology test-topology doesn't have a level matching the preferred(nonexistent) specified for the job test-job",
		},
		{
			name: "required placement at first level",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "rack",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"rack",
			},
			expectedError: "",
		},
		{
			name: "preferred placement at first level",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"rack",
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "preferred placement at middle level",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							PreferredTopologyLevel: "zone",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"zone",
				"datacenter",
				"root",
			},
			expectedError: "",
		},
		{
			name: "single level topology with preferred placement",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							PreferredTopologyLevel: "zone",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
			},
			expectedLevels: []string{
				"zone",
				"root",
			},
			expectedError: "",
		},
		{
			name: "complex topology with multiple levels",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "region",
							PreferredTopologyLevel: "zone",
						},
					},
				},
			},
			jobTopologyName: "test-topology",
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
			},
			expectedLevels: []string{
				"zone",
				"region",
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &topologyPlugin{}

			result, err := plugin.calculateRelevantDomainLevels(tt.job, tt.jobTopologyName, tt.topologyTree)

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
	tests := []struct {
		name                       string
		job                        *jobs_fake.TestJobBasic
		allocatedPodGroups         []*jobs_fake.TestJobBasic
		nodes                      map[string]nodes_fake.TestNodeBasic
		nodesToDomains             map[string]TopologyDomainID
		setupTopologyTree          func() *TopologyInfo
		expectedMaxAllocatablePods int
		expectedDomains            map[TopologyDomainID]*TopologyDomainInfo
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
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"rack": {
							"rack1.zone1": {
								ID:    "rack1.zone1",
								Name:  "rack1",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
							"rack2.zone1": {
								ID:    "rack2.zone1",
								Name:  "rack2",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = map[TopologyDomainID]*TopologyDomainInfo{
					"rack1.zone1": tree.DomainsByLevel["rack"]["rack1.zone1"],
					"rack2.zone1": tree.DomainsByLevel["rack"]["rack2.zone1"],
				}
				tree.DomainsByLevel["rack"]["rack1.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]
				tree.DomainsByLevel["rack"]["rack2.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedMaxAllocatablePods: 4,
			expectedDomains: map[TopologyDomainID]*TopologyDomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Name:            "rack1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Name:            "rack2",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"zone1": {
					ID:              "zone1",
					Name:            "zone1",
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
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack2.zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"rack": {
							"rack1.zone1": {
								ID:    "rack1.zone1",
								Name:  "rack1",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
							"rack2.zone1": {
								ID:    "rack2.zone1",
								Name:  "rack2",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = map[TopologyDomainID]*TopologyDomainInfo{
					"rack1.zone1": tree.DomainsByLevel["rack"]["rack1.zone1"],
					"rack2.zone1": tree.DomainsByLevel["rack"]["rack2.zone1"],
				}
				tree.DomainsByLevel["rack"]["rack1.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]
				tree.DomainsByLevel["rack"]["rack2.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedMaxAllocatablePods: 2,
			expectedDomains: map[TopologyDomainID]*TopologyDomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Name:            "rack1",
					Level:           "rack",
					AllocatablePods: 1, // Can only fit 1 pod
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Name:            "rack2",
					Level:           "rack",
					AllocatablePods: 1, // Can only fit 1 pod
				},
				"zone1": {
					ID:              "zone1",
					Name:            "zone1",
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
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "rack1.zone1",
				"node-2": "rack1.zone1",
				"node-3": "rack2.zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
								{NodeLabel: "rack"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"rack": {
							"rack1.zone1": {
								ID:    "rack1.zone1",
								Name:  "rack1",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
							"rack2.zone1": {
								ID:    "rack2.zone1",
								Name:  "rack2",
								Level: "rack",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				// Set parent relationships
				tree.DomainsByLevel["zone"]["zone1"].Children = map[TopologyDomainID]*TopologyDomainInfo{
					"rack1.zone1": tree.DomainsByLevel["rack"]["rack1.zone1"],
					"rack2.zone1": tree.DomainsByLevel["rack"]["rack2.zone1"],
				}
				tree.DomainsByLevel["rack"]["rack1.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]
				tree.DomainsByLevel["rack"]["rack2.zone1"].Parent = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedMaxAllocatablePods: 4,
			expectedDomains: map[TopologyDomainID]*TopologyDomainInfo{
				"rack1.zone1": {
					ID:              "rack1.zone1",
					Name:            "rack1",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"rack2.zone1": {
					ID:              "rack2.zone1",
					Name:            "rack2",
					Level:           "rack",
					AllocatablePods: 2,
				},
				"zone1": {
					ID:              "zone1",
					Name:            "zone1",
					Level:           "zone",
					AllocatablePods: 4,
				},
			},
		},
		{
			name: "no leaf domains - no allocateable domains",
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
			nodesToDomains: map[string]TopologyDomainID{
				"node-1": "zone1",
			},
			setupTopologyTree: func() *TopologyInfo {
				tree := &TopologyInfo{
					Name: "test-topology",
					TopologyResource: &kueuev1alpha1.Topology{
						Spec: kueuev1alpha1.TopologySpec{
							Levels: []kueuev1alpha1.TopologyLevel{
								{NodeLabel: "zone"},
							},
						},
					},
					DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
						"zone": {
							"zone1": {
								ID:    "zone1",
								Name:  "zone1",
								Level: "zone",
								Nodes: map[string]*node_info.NodeInfo{},
							},
						},
					},
				}

				tree.Root = tree.DomainsByLevel["zone"]["zone1"]

				return tree
			},
			expectedMaxAllocatablePods: 0,
			expectedDomains:            map[TopologyDomainID]*TopologyDomainInfo{
				// No domains should have allocations since no nodes can accommodate the job
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobName := tt.job.Name
			clusterPodGroups := append(tt.allocatedPodGroups, tt.job)
			jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(clusterPodGroups)
			nodesInfoMap := nodes_fake.BuildNodesInfoMap(tt.nodes, tasksToNodeMap)
			job := jobsInfoMap[common_info.PodGroupID(jobName)]

			topologyTree := tt.setupTopologyTree()
			for nodeName, domainId := range tt.nodesToDomains {
				nodeInfo := nodesInfoMap[nodeName]
				leafLevel := len(topologyTree.TopologyResource.Spec.Levels) - 1
				domain := topologyTree.DomainsByLevel[topologyTree.TopologyResource.Spec.Levels[leafLevel].NodeLabel][domainId]
				for domain != nil {
					if nodeInfo.Node.Labels == nil {
						nodeInfo.Node.Labels = map[string]string{
							domain.Level: domain.Name,
						}
					} else {
						nodeInfo.Node.Labels[domain.Level] = domain.Name
					}
					domain.AddNode(nodeInfo)
					domain = domain.Parent
				}
			}

			session := &framework.Session{
				Nodes:         nodesInfoMap,
				PodGroupInfos: jobsInfoMap,
				Topologies:    []*kueuev1alpha1.Topology{topologyTree.TopologyResource},
			}
			plugin := &topologyPlugin{
				sessionStateGetter: session,
				nodesInfos:         nodesInfoMap,
			}

			// Call the function under test
			maxAllocatablePods, err := plugin.calcTreeAllocatable(job, topologyTree)
			if err != nil {
				t.Errorf("failed to calc tree allocatable. job: %s, error: %v", job.PodGroup.Name, err)
			}

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

func TestTopologyPlugin_getBestJobAllocatableDomains(t *testing.T) {
	tests := []struct {
		name            string
		job             *podgroup_info.PodGroupInfo
		topologyTree    *TopologyInfo
		taskOrderFunc   common_info.LessFn
		expectedDomains []*TopologyDomainInfo
		expectedError   string
	}{
		{
			name: "single domain with minimum distance",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 2).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "zone",
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"rack": {
						"rack1.zone1": {
							ID:              "rack1.zone1",
							Name:            "rack1",
							Level:           "rack",
							AllocatablePods: 2,
						},
						"rack2.zone1": {
							ID:              "rack2.zone1",
							Name:            "rack2",
							Level:           "rack",
							AllocatablePods: 1,
						},
					},
					"zone": {
						"zone1": {
							ID:              "zone1",
							Name:            "zone1",
							Level:           "zone",
							AllocatablePods: 3,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*TopologyDomainInfo{
				{
					ID:              "rack1.zone1",
					Name:            "rack1",
					Level:           "rack",
					AllocatablePods: 2,
				},
			},
			expectedError: "",
		},
		{
			name: "no domains can allocate the job",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 2).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "zone",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"rack": {
						"rack1.zone1": {
							ID:              "rack1.zone1",
							Name:            "rack1",
							Level:           "rack",
							AllocatablePods: 1, // Can only fit 1 pod, job needs 2
						},
					},
					"zone": {
						"zone1": {
							ID:              "zone1",
							Name:            "zone1",
							Level:           "zone",
							AllocatablePods: 1, // Can only fit 1 pod, job needs 2
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*TopologyDomainInfo{},
			expectedError:   "no domains found for the job test-job, workload topology name: test-topology",
		},
		{
			name: "no relevant domain levels",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 1).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "zone",
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"datacenter": {
						"datacenter1": {
							ID:              "datacenter1",
							Name:            "datacenter1",
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
			expectedError:   "the topology test-topology doesn't have a level matching the required(zone) specified for the job test-job",
		},
		{
			name: "complex topology with multiple levels",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 3).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "region",
							PreferredTopologyLevel: "zone",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"rack": {
						"rack1.zone1.region1": {
							ID:              "rack1.zone1.region1",
							Name:            "rack1",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack2.zone1.region1": {
							ID:              "rack2.zone1.region1",
							Name:            "rack2",
							Level:           "rack",
							AllocatablePods: 3,
						},
					},
					"zone": {
						"zone1.region1": {
							ID:              "zone1.region1",
							Name:            "zone1",
							Level:           "zone",
							AllocatablePods: 6,
						},
					},
					"region": {
						"region1": {
							ID:              "region1",
							Name:            "region1",
							Level:           "region",
							AllocatablePods: 9,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*TopologyDomainInfo{
				{
					ID:              "zone1.region1",
					Name:            "zone1",
					Level:           "zone",
					AllocatablePods: 6,
				},
			},
			expectedError: "",
		},
		{
			name: "mixed task statuses - some pending, some running",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 2).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Running, NodeName: "node1"},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "zone",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"zone": {
						"zone1": {
							ID:    "zone1",
							Name:  "zone1",
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
			expectedDomains: []*TopologyDomainInfo{
				{
					ID:              "zone1",
					Name:            "zone1",
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
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 2).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Running, NodeName: "node2"},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel: "zone",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "zone"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"zone": {
						"zone1": {
							ID:    "zone1",
							Name:  "zone1",
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
							Name:  "zone2",
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
			expectedDomains: []*TopologyDomainInfo{
				{
					ID:              "zone2",
					Name:            "zone2",
					Level:           "zone",
					AllocatablePods: 2,
				},
			},
			expectedError: "",
		},
		{
			name: "Return children subset",
			job: &podgroup_info.PodGroupInfo{
				Name: "test-job",
				SubGroups: map[string]*podgroup_info.SubGroupInfo{
					podgroup_info.DefaultSubGroup: podgroup_info.NewSubGroupInfo(podgroup_info.DefaultSubGroup, 4).WithPodInfos(map[common_info.PodID]*pod_info.PodInfo{
						"pod1": {UID: "pod1", Name: "pod1", Status: pod_status.Pending},
						"pod2": {UID: "pod2", Name: "pod2", Status: pod_status.Pending},
						"pod3": {UID: "pod3", Name: "pod3", Status: pod_status.Pending},
						"pod4": {UID: "pod4", Name: "pod4", Status: pod_status.Pending},
					}),
				},
				PodGroup: &enginev2alpha2.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-job",
					},
					Spec: enginev2alpha2.PodGroupSpec{
						TopologyConstraint: enginev2alpha2.TopologyConstraint{
							RequiredTopologyLevel:  "region",
							PreferredTopologyLevel: "rack",
						},
					},
				},
			},
			topologyTree: &TopologyInfo{
				Name: "test-topology",
				TopologyResource: &kueuev1alpha1.Topology{
					Spec: kueuev1alpha1.TopologySpec{
						Levels: []kueuev1alpha1.TopologyLevel{
							{NodeLabel: "datacenter"},
							{NodeLabel: "region"},
							{NodeLabel: "zone"},
							{NodeLabel: "rack"},
						},
					},
				},
				DomainsByLevel: map[string]map[TopologyDomainID]*TopologyDomainInfo{
					"rack": {
						"rack1.zone1.region1": {
							ID:              "rack1.zone1.region1",
							Name:            "rack1",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack2.zone1.region1": {
							ID:              "rack2.zone1.region1",
							Name:            "rack2",
							Level:           "rack",
							AllocatablePods: 3,
						},
						"rack1.zone2.region1": {
							ID:              "rack1.zone2.region1",
							Name:            "rack1",
							Level:           "rack",
							AllocatablePods: 2,
						},
						"rack2.zone2.region1": {
							ID:              "rack2.zone1.region1",
							Name:            "rack2",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack3.zone3.region1": {
							ID:              "rack3.zone2.region1",
							Name:            "rack3",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack4.zone2.region1": {
							ID:              "rack4.zone2.region1",
							Name:            "rack4",
							Level:           "rack",
							AllocatablePods: 1,
						},
						"rack5.zone2.region1": {
							ID:              "rack5.zone2.region1",
							Name:            "rack5",
							Level:           "rack",
							AllocatablePods: 1,
						},
					},
					"zone": {
						"zone1.region1": {
							ID:              "zone1.region1",
							Name:            "zone1",
							Level:           "zone",
							AllocatablePods: 6,
							Children: map[TopologyDomainID]*TopologyDomainInfo{
								"rack1.zone1.region1": {
									ID:              "rack1.zone1.region1",
									Name:            "rack1",
									Level:           "rack",
									AllocatablePods: 3,
								},
								"rack2.zone1.region1": {
									ID:              "rack2.zone1.region1",
									Name:            "rack2",
									Level:           "rack",
									AllocatablePods: 3,
								},
							},
						},
						"zone2.region1": {
							ID:              "zone2.region1",
							Name:            "zone2",
							Level:           "zone",
							AllocatablePods: 6,
							Children: map[TopologyDomainID]*TopologyDomainInfo{
								"rack1.zone2.region1": {
									ID:              "rack1.zone2.region1",
									Name:            "rack1",
									Level:           "rack",
									AllocatablePods: 2,
								},
								"rack2.zone1.region1": {
									ID:              "rack2.zone1.region1",
									Name:            "rack2",
									Level:           "rack",
									AllocatablePods: 1,
								},
								"rack3.zone2.region1": {
									ID:              "rack3.zone2.region1",
									Name:            "rack3",
									Level:           "rack",
									AllocatablePods: 1,
								},
								"rack4.zone2.region1": {
									ID:              "rack4.zone2.region1",
									Name:            "rack4",
									Level:           "rack",
									AllocatablePods: 1,
								},
								"rack5.zone2.region1": {
									ID:              "rack5.zone2.region1",
									Name:            "rack5",
									Level:           "rack",
									AllocatablePods: 1,
								},
							},
						},
					},
					"region": {
						"region1": {
							ID:              "region1",
							Name:            "region1",
							Level:           "region",
							AllocatablePods: 9,
						},
					},
				},
			},
			taskOrderFunc: func(l, r interface{}) bool {
				return l.(*pod_info.PodInfo).Name < r.(*pod_info.PodInfo).Name
			},
			expectedDomains: []*TopologyDomainInfo{
				{
					ID:              "rack1.zone1.region1",
					Name:            "rack1",
					Level:           "rack",
					AllocatablePods: 3,
				},
				{
					ID:              "rack2.zone1.region1",
					Name:            "rack2",
					Level:           "rack",
					AllocatablePods: 3,
				},
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &topologyPlugin{
				taskOrderFunc: tt.taskOrderFunc,
			}

			result, err := plugin.getBestJobAllocatableDomains(tt.job, tt.topologyTree)

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
			if len(result) != len(tt.expectedDomains) {
				t.Errorf("expected %d domains, but got %d", len(tt.expectedDomains), len(result))
				return
			}

			// Sort both slices by domain ID for consistent comparison
			sortDomains := func(domains []*TopologyDomainInfo) {
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
				if actualDomain.Name != expectedDomain.Name {
					t.Errorf("domain %d: expected Name %s, got %s", i, expectedDomain.Name, actualDomain.Name)
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
