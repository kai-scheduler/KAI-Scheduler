// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
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
		{
			// MinNodeGPUMemoryMiB is used by GetTasksToAllocateInitResourceVector to convert GPU-memory
			// requests to GPU fractions for CanReclaimResources: allocated + fraction ≤ fairShare.
			//
			// The node has 800 MiB/GPU → minNodeGPUMemoryMiB=800 → reclaimer fraction = 320/800 = 0.40 ≤ 0.5
			// → CanReclaimResources passes → reclaim proceeds.
			//
			// If minNodeGPUMemoryMiB were wrong (for example, 100):
			//   fraction = 320/100 = 3.2 > 0.5 → CanReclaimResources returns false → reclaim blocked.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim enabled when min node GPU memory produces fraction within fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "q0_running",
						RequiredGpuMemory: 720,
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
						RequiredGpuMemory: 320,
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
					"node0": {GPUs: 1, GPUMemory: 800},
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
			// 320 MiB fits only on node0 (800 MiB/GPU), where it costs 0.4 GPU of queue1's 1 GPU.
			// Priced on the smallest GPU in the cluster (160 MiB) it would cost 2 GPUs.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim a large GPU for a gpu memory request while a smaller GPU is idle",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_borrower",
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
						Name:              "q1_pending",
						RequiredGpuMemory: 320,
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
					"node0": {GPUs: 1, GPUMemory: 800},
					"node1": {GPUs: 1, GPUMemory: 160},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_borrower": {
						Status:       pod_status.Pipelined,
						NodeName:     "node1",
						GPUsRequired: 1,
					},
					"q1_pending": {
						Status:               pod_status.Pipelined,
						NodeName:             "node0",
						GPUsRequired:         0,
						DontValidateGPUGroup: true,
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
			// queue1 already uses 0.5 GPU of its 1 GPU fair share. q1_pending may only run on node0,
			// where 320 MiB costs 0.8 GPU, so reclaiming node0 would take queue1 to 1.3 GPUs.
			// Priced on the largest GPU in the cluster it would cost 0.4 GPU.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no reclaim when a gpu memory request costs more than the fair share on its node",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q1_running",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:  "node1",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                "q0_node0",
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
						Name:                "q0_node2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q2_pending",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Pending,
								NodeAffinityNames: []string{"missing-node"},
							},
						},
					},
					{
						Name:              "q1_pending",
						RequiredGpuMemory: 320,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Pending,
								NodeAffinityNames: []string{"node0"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1, GPUMemory: 400},
					"node1": {GPUs: 1, GPUMemory: 800},
					"node2": {GPUs: 1, GPUMemory: 800},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue2",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_node0": {
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUsRequired: 1,
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
	}
}
