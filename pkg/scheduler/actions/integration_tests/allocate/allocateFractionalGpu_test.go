// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestAllocateFractionalGpuIntegrationTest(t *testing.T) {
	defer gock.Off()

	integration_tests_utils.RunTests(t, getAllocateFractionalGpuTestsMetadata())
}
func getAllocateFractionalGpuTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 shared gpu job running, 1 pending interactive shared gpu job - allocate to new gpu",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.6,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								NodeName:  "node0",
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
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
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_shared_gpu_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.6,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job1": {
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 whole gpu job running, 1 pending interactive shared gpu job - allocate",
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
						Name:                "pending_shared_gpu_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
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
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job1": {
						GPUsRequired:         0.5,
						NodeName:             "node0",
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 interactive shared gpu job running, 4 pending interactive shared gpus pending - allocate 3 of the shared GPUs jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								NodeName:  "node0",
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job0",
						RequiredGPUsPerTask: 0.6,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job2",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job3",
						RequiredGPUsPerTask: 0.3,
						Priority:            constants.PriorityBuildNumber,
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
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_shared_gpu_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job0": {
						GPUsRequired: 0.6,
						GPUGroups:    []string{"1"},
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job2": {
						GPUsRequired: 0.5,
						Status:       pod_status.Pending,
					},
					"pending_shared_gpu_job3": {
						GPUsRequired: 0.3,
						GPUGroups:    []string{"1"},
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{

						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allow allocation of fractional training when over quota with fractional jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.5,
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
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{

						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allow allocation of fractional training when over quota with fractional jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.5,
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
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{

						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 0,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Basic request gpu by memory when cluster is empty",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "pending_job-0",
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
					"pending_job-0": {
						NodeName:  "node0",
						Status:    pod_status.Running,
						GPUGroups: []string{"0"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{

						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			RoundsUntilMatch: 2,
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "test gpuSharingOrder - one node empty and one node with already running frac job - allocate to the node with already running job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								NodeName:  "node1",
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_shared_gpu_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
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
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_shared_gpu_job0": {
						NodeName:     "node1",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job1": {
						NodeName:     "node1",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{

						NumberOfCacheBinds: 5,
					},
				},
			},
		},
	}
}
