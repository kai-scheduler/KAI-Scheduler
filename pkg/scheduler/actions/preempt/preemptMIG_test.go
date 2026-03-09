// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestMIGPreempt(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getMIGTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running Test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getMIGTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Build preempts train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job-0",
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
						Name:      "pending_job-0",
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
					"running_job-0": {
						Status:   pod_status.Releasing,
						NodeName: "node0",
					},
					"pending_job-0": {
						Status:   pod_status.Pipelined,
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
						Name:      "running_job-0",
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
						Name:      "pending_job-0",
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
					"running_job-0": {
						Status:   pod_status.Releasing,
						NodeName: "node0",
					},
					"pending_job-0": {
						Status:   pod_status.Pipelined,
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
						Name:      "running_job-0",
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
						Name:      "running_job-1",
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
						Name:      "running_job-2",
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
						Name:      "pending_job-0",
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
					"running_job-0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"running_job-1": {
						Status:   pod_status.Releasing,
						NodeName: "node0",
					},
					"running_job-2": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job-0": {
						Status:   pod_status.Pipelined,
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
						Name:      "running_job-0",
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
						Name:      "pending_job-0",
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
					"running_job-0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job-0": {
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
						Name:      "running_job-0",
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
						Name:      "pending_job-0",
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
					"running_job-0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job-0": {
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
