// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleReclaimGpuMemory(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	testsMetadata := getReclaimGpuMemoryTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		reclaimAction := reclaim.New()
		reclaimAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getReclaimGpuMemoryTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "fair share reclaim for pending gpu memory job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_running",
						RequiredGpuMemory: 90,
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
						Name:              "q1_pending",
						RequiredGpuMemory: 40,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0.5,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 0.5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_running": {
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"q1_pending": {
						Status:               pod_status.Pipelined,
						GPUsRequired:         0,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim minimal lower priority victims across queues",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_train_a",
						RequiredGpuMemory: 60,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:              "q0_train_b",
						RequiredGpuMemory: 80,
						Priority:          constants.PriorityTrainNumber - 1,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node1",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:              "q1_running",
						RequiredGpuMemory: 30,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node0",
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:              "q1_pending",
						RequiredGpuMemory: 50,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
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
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_train_a": {
						GPUsRequired:         0,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"q0_train_b": {
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
						NodeName:     "node1",
					},
					"q1_running": {
						GPUsRequired:         0,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"q1_pending": {
						GPUsRequired:         0,
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "multiple victims reclaimed by priority",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_train_a",
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
						Name:              "q0_train_b",
						RequiredGpuMemory: 30,
						Priority:          constants.PriorityTrainNumber - 1,
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
						Name:              "q0_train_c",
						RequiredGpuMemory: 40,
						Priority:          constants.PriorityTrainNumber - 2,
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
						Name:              "q1_pending",
						RequiredGpuMemory: 80,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0.2,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 0.8,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_train_a": {
						Status:       pod_status.Running,
						GPUsRequired: 0,
						NodeName:     "node0",
					},
					"q0_train_b": {
						Status:       pod_status.Releasing,
						GPUsRequired: 0,
						NodeName:     "node0",
					},
					"q0_train_c": {
						Status:       pod_status.Releasing,
						GPUsRequired: 0,
						NodeName:     "node0",
					},
					"q1_pending": {
						Status:               pod_status.Pipelined,
						GPUsRequired:         0,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no reclaim when queue at deserved",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_running",
						RequiredGpuMemory: 50,
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
						Name:              "q1_pending",
						RequiredGpuMemory: 60,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0.5,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 0.5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_running": {
						Status:       pod_status.Running,
						GPUsRequired: 0,
						NodeName:     "node0",
					},
					"q1_pending": {
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
				Name: "reclaim whole gpu for fractional request within fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_whole_gpu",
						RequiredGpuMemory: 100,
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
						Name:              "q1_pending",
						RequiredGpuMemory: 50,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0.5,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 0.5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_whole_gpu": {
						Status:       pod_status.Releasing,
						GPUsRequired: 0,
						NodeName:     "node0",
					},
					"q1_pending": {
						Status:               pod_status.Pipelined,
						GPUsRequired:         0,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
	}
}
