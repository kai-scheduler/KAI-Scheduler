// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleMIGAllocation(t *testing.T) {
	integration_tests_utils.RunTests(t, getMIGTestsMetadata())
}

func getMIGTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MIG job requesting unavailable MIG device",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MIG job requesting single MIG device",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job": {
						Status: pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MIG job requesting MIG device on node with no available MIG devices",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 0,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MIG job requesting MIG device on node with Legacy MIG jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								IsLegacyMigTask: true,
								State:           pod_status.Running,
								NodeName:        "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 2,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pending legacy MIG job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								IsLegacyMigTask: true,
								State:           pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 2,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MIG job with multiple tasks requesting MIG device",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Priority:  constants.PriorityBuildNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 2,
								},
								State: pod_status.Pending,
							},
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:      "pending_job1",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State: pod_status.Pending,
							},
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:      "pending_job2",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb":  2,
							"nvidia.com/mig-1g.10gb": 3,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status: pod_status.Running,
					},
					"pending_job1": {
						Status: pod_status.Pending,
					},
					"pending_job2": {
						Status: pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
			},
		},
	}
}
