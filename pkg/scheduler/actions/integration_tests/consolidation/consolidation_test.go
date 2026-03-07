// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"

	kaiv1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/topology_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConsolidateIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getConsolidateTestsMetadata())
}

func getConsolidateTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 train job of 1 GPU running on node0, 1 train job of 1 GPU running on node 0, 1 pending job of 2 GPUs - consolidate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      2,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 build job of 1 GPU running on node0, 1 build job of 1 GPU running on node 0, 1 pending job of 2 GPUs - don't consolidate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:     0,
						NumberOfCacheEvictions: 0,
					},
				},
			},
			RoundsUntilMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "topology consolidation with required - simple",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job_zone1_rack1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone1_rack2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone1_rack3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "zone",
							},
						),
					},
				},
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "zone",
								},
								{
									NodeLabel: "rack",
								},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"zone1-rack1": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack1",
						},
					},
					"zone1-rack2": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack2",
						},
					},
					"zone1-rack3": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack3",
						},
					},
					"zone2-rack1": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack1",
						},
					},
					"zone2-rack2": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack2",
						},
					},
					"zone2-rack3": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack3",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_zone1_rack1": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone1_rack2": {
						NodeName:     "zone1-rack2",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone1_rack3": {
						NodeName:     "zone1-rack3",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack1": {
						NodeName:     "zone2-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack2": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack3": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 6,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 4,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "required topology consolidation with non-feasible nodes",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job_zone1_rack1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone1_rack2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone1_rack3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone1-rack3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job_zone2_rack3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "zone2-rack3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
						RootSubGroupSet: subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
							&topology_info.TopologyConstraintInfo{
								Topology:      "cluster-topology",
								RequiredLevel: "zone",
							},
						),
					},
				},
				Topologies: []*kaiv1alpha1.Topology{
					{
						ObjectMeta: v1.ObjectMeta{
							Name: "cluster-topology",
						},
						Spec: kaiv1alpha1.TopologySpec{
							Levels: []kaiv1alpha1.TopologyLevel{
								{
									NodeLabel: "zone",
								},
								{
									NodeLabel: "rack",
								},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"zone1-rack1": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack1",
						},
					},
					"zone1-rack2": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack2",
						},
					},
					"zone1-rack3": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone1",
							"rack": "rack3",
						},
					},
					"zone2-rack1": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack1",
						},
					},
					"zone2-rack2": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack2",
						},
					},
					"zone2-rack3": {
						GPUs: 3,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack3",
						},
					},
					"sneaky-node": {
						GPUs: 0,
						Labels: map[string]string{
							"zone": "zone2",
							"rack": "rack3",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_zone1_rack1": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone1_rack2": {
						NodeName:     "zone1-rack2",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone1_rack3": {
						NodeName:     "zone1-rack3",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack1": {
						NodeName:     "zone2-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack2": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job_zone2_rack3": {
						NodeName:     "zone1-rack1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 6,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 4,
					},
				},
			},
		},
	}
}
