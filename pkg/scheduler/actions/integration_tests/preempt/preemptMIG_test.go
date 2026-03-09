// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestMIGPreempt(t *testing.T) {
	integration_tests_utils.RunTests(t, getMIGTestsMetadata())
}

func getMIGTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Build preempts train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityBuildNumber,
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
					"running_job": {
						Status: pod_status.Pending,
					},
					"pending_job": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference preempts train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityInferenceNumber,
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
					"running_job": {
						Status: pod_status.Pending,
					},
					"pending_job": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Several jobs with different priorities preempt one",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Priority:  constants.PriorityBuildNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "running_job1",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "running_job2",
						Priority:  constants.PriorityBuildNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityBuildNumber,
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
							"nvidia.com/mig-1g.10gb": 3,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"running_job1": {
						Status: pod_status.Pending,
					},
					"running_job2": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Lower priority job should not preempt",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityBuildNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityBuildNumber,
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
					"running_job": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Train job runs in a different queue - should not preempt",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job",
						Priority:  constants.PriorityBuildNumber,
						QueueName: "queue1",
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
					{
						Name:         "queue1",
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
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
		},
	}
}
