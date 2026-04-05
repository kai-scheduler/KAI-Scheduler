// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandlePreemptElastic(t *testing.T) {

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getElasticTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running Test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getElasticTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts single task from elastic train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts several tasks from elastic train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInferenceNumber,
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts all tasks from elastic train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInferenceNumber,
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts running job and a single task from elastic train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
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
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInferenceNumber,
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
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts single task from several elastic train jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInferenceNumber,
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts several jobs (one of them is elastic)",
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
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityInferenceNumber,
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1-0": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job2-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 4,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  4,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Elastic inference job preempts train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Releasing,
					},
					"pending_job0-0": {
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Elastic inference job preempts task from train job in favor of a single task",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
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
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Inference job preempts single task, moves one task from elastic train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInferenceNumber,
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
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"running_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
	}
}
