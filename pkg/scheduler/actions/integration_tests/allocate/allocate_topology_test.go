// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestAllocateTopologyIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getTopologyAllocateTestsMetadata())
}

func getTopologyAllocateTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Multi-level topology - pack job in single rack with preferred level",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "datacenter-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/zone",
								},
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "datacenter-topology",
								RequiredLevel:  "topology.kubernetes.io/zone",
								PreferredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone1",
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone1",
							"topology.kubernetes.io/rack": "rack2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone2",
							"topology.kubernetes.io/rack": "rack3",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 6,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 3,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 3,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Topology packing - prefer domains with existing allocations",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 3,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 3,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 6,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 3,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Topology constraint - job cannot fit in single domain, stays pending",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired:         4,
						Status:               pod_status.Pending,
						ExpectedErrorMessage: "\nPodSchedulingErrors.\ntopology rack-topology, requirement topology.kubernetes.io/rack couldn't be satisfied for job </pending_job0>: not enough resources in the cluster to allocate the job.",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Three-level topology - allocate with required and preferred constraints",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "complex-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/region",
								},
								{
									NodeLabel: "topology.kubernetes.io/zone",
								},
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "complex-topology",
								RequiredLevel:  "topology.kubernetes.io/region",
								PreferredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/region": "us-west",
							"topology.kubernetes.io/zone":   "us-west-1a",
							"topology.kubernetes.io/rack":   "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/region": "us-west",
							"topology.kubernetes.io/zone":   "us-west-1a",
							"topology.kubernetes.io/rack":   "rack2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/region": "us-west",
							"topology.kubernetes.io/zone":   "us-west-1b",
							"topology.kubernetes.io/rack":   "rack3",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/region": "us-east",
							"topology.kubernetes.io/zone":   "us-east-1a",
							"topology.kubernetes.io/rack":   "rack4",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 8,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 4,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Topology with partial allocation - one rack full, allocate remaining in another",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Running,
						NodeName:     "node2",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preferred topology only - still allocates when preferred level cannot be satisfied",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:       "rack-topology",
								PreferredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
					"running_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
						NodeName:     "node1",
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Topology with CPU constraints - allocate within topology domain respecting CPU limits",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						RequiredCPUsPerTask: 2000,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      2,
						CPUMillis: 5000,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs:      2,
						CPUMillis: 1000,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Topology constraint failure - sufficient cluster resources but fragmented across domains",
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "rack-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "topology.kubernetes.io/rack",
								},
							},
						},
					},
				},
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "rack-topology",
								RequiredLevel: "topology.kubernetes.io/rack",
							},
						),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack1",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack2",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack3",
						},
					},
					"node3": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/rack": "rack4",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 8,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired:         4,
						Status:               pod_status.Pending,
						ExpectedErrorMessage: "\nUnable to schedule podgroup.\n<rack1>: node-group rack1 can allocate only 2 of 4 required pods.\n<rack2>: node-group rack2 can allocate only 2 of 4 required pods.\n<rack3>: node-group rack3 can allocate only 2 of 4 required pods.\n<rack4>: node-group rack4 can allocate only 2 of 4 required pods.\ntopology rack-topology, requirement topology.kubernetes.io/rack couldn't be satisfied for job </pending_job0>.",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
	}
}
