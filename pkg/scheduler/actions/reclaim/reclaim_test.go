// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleReclaim(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()
	testsMetadata := getTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		reclaimAction := reclaim.New()
		reclaimAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim jobs according to queue fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_running_job0",
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
						Name:                "q0_running_job1",
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
						Name:                "q0_running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber - 1,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber - 1,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "reclaimer",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "reclaimer_queue",
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
						GPUs: 6,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d1",
					},
					{
						Name:               "reclaimer_queue",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"q0_running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"q0_running_job2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"q1_running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"q1_running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"q1_running_job2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"reclaimer": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue1 is under deserved quota, queue0 is over deserved quota and will go below quota - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_running_job",
						RequiredGPUsPerTask: 10,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_pending_job",
						RequiredGPUsPerTask: 10,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 16,
					},
					"node1": {
						GPUs: 16,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_running_job": {
						GPUsRequired:         20,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q1_pending_job": {
						NodeName:             "node0",
						GPUsRequired:         10,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      2,
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue1 is under deserved quota, queue0 is over deserved quota but under fair share - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job3",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job4",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node4",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job5",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node5",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_pending_job6",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "q0_pending_job7",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "q0_pending_job8",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "q1_pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
					"node1": {
						GPUs: 3,
					},
					"node2": {
						GPUs: 3,
					},
					"node3": {
						GPUs: 3,
					},
					"node4": {
						GPUs: 3,
					},
					"node5": {
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_running_job0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_running_job1": {
						NodeName:             "node1",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_running_job2": {
						NodeName:             "node2",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_running_job3": {
						NodeName:             "node3",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_running_job4": {
						NodeName:             "node4",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_running_job5": {
						NodeName:             "node5",
						GPUsRequired:         2,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q0_pending_job6": {
						GPUsRequired:         2,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"q0_pending_job7": {
						GPUsRequired:         2,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"q0_pending_job8": {
						GPUsRequired:         2,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"q1_pending_job0": {
						NodeName:             "node5",
						GPUsRequired:         2,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is under fair share, queue1 is over fair share - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
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
						Name:                "q0_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job2",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "q1_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 3,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						NodeName:             "node0",
						GPUsRequired:         0.5,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job2": {
						NodeName:             "node0",
						GPUsRequired:         0.5,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q1_job1": {
						GPUsRequired:         0.5,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is over fair share, queue1 is under fair share - reclaim for job with multiple tasks",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
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
						Name:                "q0_job1",
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
						Name:                "q0_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
					"node2": {
						GPUs: 1,
					},
					"node3": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q0_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q1_job1": {
						GPUsRequired:         4,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      4,
						NumberOfCacheEvictions:  4,
						NumberOfPipelineActions: 4,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is over fair share, queue1 is under fair share - reclaim for job with multiple tasks from job with multiple tasks",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
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
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
					"node2": {
						GPUs: 1,
					},
					"node3": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						GPUsRequired:         3,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q1_job1": {
						GPUsRequired:         3,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      4,
						NumberOfCacheEvictions:  4,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim from multiple queues running on multiple nodes for a job with multiple tasks",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
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
						Name:                "job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "job2",
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
						Name:                "job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
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
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       3,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"job1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"job2": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"job3": {
						NodeName:             "node1",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending_job": {
						GPUsRequired:         3,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      3,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 jobs from queue0 running on node0 - 2 pending jobs from queue1 - reclaim",
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
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 job from queue0 and 1 job from queue1 running on node0 - don't reclaim",
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
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Don't reclaim when there's fairness between the queues",
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
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 jobs from queue0 running on node0 (1 train, 1 build)- 2 pending jobs from queue1 - reclaim train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "running_job1",
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
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Queues are over provisioned, ratio between queues are equal, yet don't reclaim since number of used GPUs is equal to the deserved GPUs",
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
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
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
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
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
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 train job of 2 GPUs from queue0 running on node0 with deserved of 1, 2 pending jobs from queue1 - reclaim",
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
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 train job of 2 GPUs from queue0 running on node0 with deserved of 1, 2 pending jobs (1 build) from queue1 - reclaim",
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
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 train job of 1 GPUs from queue0 running on node0 with deserved of 1, 1 pending build job of 2 GPUs from queue1 with deserved of 1 - don't reclaim",
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
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 train and 1 preemptible interactive are over quota - reclaim train",
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
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 preemptible interactive are over quota - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInteractivePreemptibleNumber,
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
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
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
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 interactive job is running with more GPUs than the quota, 1 interactive job form different project is pending - don't reclaim (never kill interactive jobs)",
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
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Interactive jobs should use the number of deserved GPUs, and not the reclaimable deserved GPUs",
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
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
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
						GPUsRequired: 3,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Verify inactive queues don't affect the reclaimable deserved calculation",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
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
					}, {
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
					}, {
						Name:                "running_job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
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
					"running_job2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job3": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim from job that allocated more resources than its' fairShare (no remaining)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 8,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 6,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 10,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:         8,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending_job0": {
						NodeName:             "node0",
						GPUsRequired:         6,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim from job that allocated more resources than its' fairShare (no remaining)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 8,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 6,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 10,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:         8,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending_job0": {
						NodeName:             "node0",
						GPUsRequired:         6,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "don't reclaim from job that allocated resources within its' fairShare",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 7,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 10,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 4,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 7,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:     1,
						NumberOfCacheEvictions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim from job that allocated more resources than its' fairShare (remaining was given to queue with lower over quota weight)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 10,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 7,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 12,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:         10,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending_job0": {
						NodeName:             "node0",
						GPUsRequired:         7,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is under deserved quota, queue1 is over deserved quota but not over fair share - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job1",
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
						Name:                "q1_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 3,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						NodeName:             "node1",
						GPUsRequired:         2,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is under deserved quota and submitted multiple jobs, queue1 is over deserved quota but not over fair share - reclaim single job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job1",
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
						Name:                "q1_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Pending,
							},
						},
					},
					{
						Name:                "q1_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 3,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 3,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						NodeName:             "node1",
						GPUsRequired:         2,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"q1_job1": {
						NodeName:             "node1",
						GPUsRequired:         2,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue1 is under deserved quota, queue0 has exactly deserved quota - should consolidate q0 to node0 and pipeline q1 to node1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job1",
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
						Name:                "q1_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       3,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						NodeName:             "node1",
						GPUsRequired:         2,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      2,
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Attempt to reclaim from an inference workload",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						NodeName:             "",
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DRF - reclaim from queue that has one resource over fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1500 * 10e6,
						RequiredCPUsPerTask:   1500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job_a_1",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 750 * 10e6,
						RequiredCPUsPerTask:   500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 750 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 750 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMemory: 3000 * 10e6,
						CPUMillis: 8000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
					{
						Name:           "queue1",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_a_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"running_job_a_1": {
						Status: pod_status.Releasing,
					},
					"running_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DRF - queue cannot reclaim if one of its resources is over fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   2500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 400 * 10e6,
						RequiredCPUsPerTask:   400,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "running_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 2000 * 10e6,
						RequiredCPUsPerTask:   500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job_b_1",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   500,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   1000,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMemory: 4000 * 10e6,
						CPUMillis: 4000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:             "queue0",
						DeservedCPUs:     test_utils.CreateFloat64Pointer(2000),
						MaxAllowedCPUs:   test_utils.CreateFloat64Pointer(3000),
						DeservedMemory:   test_utils.CreateFloat64Pointer(2000),
						MaxAllowedMemory: test_utils.CreateFloat64Pointer(3000),
					},
					{
						Name:             "queue1",
						DeservedCPUs:     test_utils.CreateFloat64Pointer(2000),
						MaxAllowedCPUs:   test_utils.CreateFloat64Pointer(3000),
						DeservedMemory:   test_utils.CreateFloat64Pointer(2000),
						MaxAllowedMemory: test_utils.CreateFloat64Pointer(3000),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_a_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_job_a_0": {
						Status: pod_status.Pending,
					},
					"running_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"running_job_b_1": {
						NodeName: "node0",
						Status:   pod_status.Running,
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DRF - queue cannot reclaim if adding job will cause queue to be over fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   1500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 400 * 10e6,
						RequiredCPUsPerTask:   1000,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "running_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 2000 * 10e6,
						RequiredCPUsPerTask:   500,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job_b_1",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   500,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 1000 * 10e6,
						RequiredCPUsPerTask:   1000,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMemory: 4000 * 10e6,
						CPUMillis: 4000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:             "queue0",
						DeservedCPUs:     test_utils.CreateFloat64Pointer(2000),
						MaxAllowedCPUs:   test_utils.CreateFloat64Pointer(3000),
						DeservedMemory:   test_utils.CreateFloat64Pointer(2000),
						MaxAllowedMemory: test_utils.CreateFloat64Pointer(3000),
					},
					{
						Name:             "queue1",
						DeservedCPUs:     test_utils.CreateFloat64Pointer(2000),
						MaxAllowedCPUs:   test_utils.CreateFloat64Pointer(3000),
						DeservedMemory:   test_utils.CreateFloat64Pointer(2000),
						MaxAllowedMemory: test_utils.CreateFloat64Pointer(3000),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_a_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_job_a_0": {
						Status: pod_status.Pending,
					},
					"running_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"running_job_b_1": {
						NodeName: "node0",
						Status:   pod_status.Running,
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "unlimited GPU quota, should not reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       -1,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 4,
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
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "unlimited GPU quota, first one is already running, should not reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						DeservedGPUs:       -1,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       -1,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 4,
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
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "unlimited GPU quota using department, should not reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						Name:               "queue0",
						GPUOverQuotaWeight: 0,
						DeservedGPUs:       -1,
						ParentQueue:        "department0",
					},
					{
						Name:               "queue1",
						GPUOverQuotaWeight: 0,
						DeservedGPUs:       2,
						ParentQueue:        "department1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "department0",
						DeservedGPUs: -1,
					},
					{
						Name:         "department1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 4,
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
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim based on releasing resources with consolidation",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
						RequiredGPUsPerTask: 0.5,
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
						Name:                "running-job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
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
					"node1": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0": {
						GPUsRequired:         0.5,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"running-job1": {
						GPUsRequired:         0.5,
						Status:               pod_status.Pipelined,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"pending-job": {
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim based on releasing resources without consolidation",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
						RequiredGPUsPerTask: 0.7,
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
						Name:                "running-job1",
						RequiredGPUsPerTask: 0.7,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
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
					"node1": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"running-job1": {
						GPUsRequired:         0.7,
						Status:               pod_status.Releasing,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending-job": {
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
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
				Name: "Reclaim based on releasing and idle resources",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job",
						RequiredGPUsPerTask: 0.7,
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Releasing,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						NodeName:             "node0",
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim based on releasing and idle resources with fractions",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
						RequiredGPUsPerTask: 0.7,
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
						Name:                "running-job1",
						RequiredGPUsPerTask: 0.3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 0.7,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Releasing,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						GPUsRequired:         0.3,
						Status:               pod_status.Running,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending-job0-0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Pipelined,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						GPUsRequired:         0.7,
						Status:               pod_status.Pipelined,
						NodeName:             "node0",
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "gang job fails to allocate enough pods",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_job0",
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
						Name:                "q0_job1",
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
						Name:                "q0_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q1_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(4),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
								NodeAffinityNames: []string{
									"unexisting-node-affinity",
								},
							},
							{
								State: pod_status.Pending,
								NodeAffinityNames: []string{
									"unexisting-node-affinity",
								},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
					"node2": {
						GPUs: 1,
					},
					"node3": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_job3": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q1_job0": {
						GPUsRequired:         5,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfCacheEvictions:  0,
						NumberOfPipelineActions: 0,
					},
				},
			},
		},
		{
			// In some cases, when multiple tasks and nodes are involved, tasks get re-written to the podgroupinfo object
			// in the session - this test is to ensure that we don't have any issues with that
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is in deserved share, queue1 is under fair share - do not reclaim - multiple tasks",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "q0_n0_job0",
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
						Name:                "q0_n0_job1",
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
						Name:                "q0_n0_job2",
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
						Name:                "q0_n0_job3",
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
						Name:                "q0_n1_job0",
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
						Name:                "q0_n1_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q0_n1_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q0_n1_job3",
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
						Name:                "q0_n2_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node2",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_n2_job1",
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
						Name:                "q0_n2_job2",
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
						Name:                "q0_n2_job3",
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
						Name:                "q0_n3_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_n3_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q0_n3_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q0_n3_job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node3",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "q1_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
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
						GPUs: 4,
					},
					"node1": {
						GPUs: 4,
					},
					"node2": {
						GPUs: 4,
					},
					"node3": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       12,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_n0_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n0_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n0_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n0_job3": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n1_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n1_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n1_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n1_job3": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n2_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n2_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n2_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n2_job3": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n3_job0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n3_job1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n3_job2": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q0_n3_job3": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"q1_job1": {
						GPUsRequired:         5,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfCacheEvictions:  0,
						NumberOfPipelineActions: 0,
					},
				},
			},
		},
	}
}
