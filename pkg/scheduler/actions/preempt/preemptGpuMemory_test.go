// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandlePreemptGpuMemory(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	testsMetadata := getPreemptGpuMemoryTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getPreemptGpuMemoryTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "build preempts train based on gpu memory",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_train",
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
						Name:              "pending_build",
						RequiredGpuMemory: 55,
						Priority:          constants.PriorityBuildNumber,
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
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_train": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"pending_build": {
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
				Name: "minimal victim preemption by priority",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_train_a",
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
						Name:              "running_train_b",
						RequiredGpuMemory: 50,
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
						Name:              "pending_build",
						RequiredGpuMemory: 55,
						Priority:          constants.PriorityBuildNumber,
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
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_train_a": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
						Status:       pod_status.Running,
					},
					"running_train_b": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"pending_build": {
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
				Name: "fractional build preempts whole gpu train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_whole_train",
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
						Name:              "pending_build",
						RequiredGpuMemory: 50,
						Priority:          constants.PriorityBuildNumber,
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
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_whole_train": {
						NodeName:     "node0",
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"pending_build": {
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
				Name: "multi task build preempts across gpus",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_train_gpu0",
						RequiredGpuMemory: 70,
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
						Name:              "running_train_gpu1",
						RequiredGpuMemory: 80,
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
						Name:              "pending_build_two_tasks",
						RequiredGpuMemory: 80,
						Priority:          constants.PriorityBuildNumber,
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
					"running_train_gpu0": {
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"running_train_gpu1": {
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
						GPUsRequired: 0,
						Status:       pod_status.Releasing,
					},
					"pending_build_two_tasks": {
						Status:               pod_status.Pipelined,
						GPUsRequired:         0,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no preemption on same priority",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "running_train",
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
						Name:              "pending_train",
						RequiredGpuMemory: 50,
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
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_train": {
						NodeName:     "node0",
						GPUsRequired: 0,
						Status:       pod_status.Running,
					},
					"pending_train": {
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
