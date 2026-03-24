// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleConsolidationGpuMemory(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	testsMetadata := getConsolidationGpuMemoryTestsMetadata()

	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test %d/%d: %s\n", testNumber, len(testsMetadata), testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		consolidationAction := consolidation.New()
		consolidationAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getConsolidationGpuMemoryTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "consolidate two memory jobs to free gpu",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_job0",
						RequiredGpuMemory: 30,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "running_job1",
						RequiredGpuMemory: 40,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"1"},
							},
						},
					},
					{
						Name:              "pending_job",
						RequiredGpuMemory: 80,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
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
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
						GPUsRequired: 0,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
						Status:       pod_status.Pipelined,
						GPUsRequired: 0,
					},
					"pending_job": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pipelined,
						GPUsRequired: 0,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no consolidation when memory does not fit",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_job0",
						RequiredGpuMemory: 60,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "running_job1",
						RequiredGpuMemory: 50,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"1"},
							},
						},
					},
					{
						Name:              "pending_job",
						RequiredGpuMemory: 80,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 2},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
						GPUsRequired: 0,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
						GPUsRequired: 0,
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
				Name: "consolidate three jobs to enable two large tasks",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_job0",
						RequiredGpuMemory: 25,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "running_job1",
						RequiredGpuMemory: 30,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node1",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "running_job2",
						RequiredGpuMemory: 35,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node2",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "pending_job",
						RequiredGpuMemory: 85,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
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
					"node0": {GPUs: 1},
					"node1": {GPUs: 1},
					"node2": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						Status:       pod_status.Running,
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
					},
					"running_job1": {
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
					},
					"running_job2": {
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job-0": {
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending_job-1": {
						Status:               pod_status.Pipelined,
						NodeName:             "node2",
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 4,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "consolidate to free gpu for whole job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_small0",
						RequiredGpuMemory: 20,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:              "running_small1",
						RequiredGpuMemory: 25,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node1",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                "pending_whole",
						RequiredGPUsPerTask: 1,
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
					"node0": {GPUs: 1},
					"node1": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_small0": {
						NodeName:     "node0",
						Status:       pod_status.Running,
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
					},
					"running_small1": {
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
					},
					"pending_whole": {
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
						GPUGroups:    []string{"0"},
						GPUsRequired: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
	}
}
