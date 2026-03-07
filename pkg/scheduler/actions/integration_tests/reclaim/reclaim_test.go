// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	"gopkg.in/h2non/gock.v1"
	"k8s.io/utils/pointer"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestReclaimIntegrationTest(t *testing.T) {
	defer gock.Off()

	integration_tests_utils.RunTests(t, getReclaimTestsMetadata())
}

func getReclaimTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
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
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
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
								NodeName:  "node0",
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
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
						Status:               pod_status.Running,
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
				Name: "1 train job running for queue0, 1 train job pending for queue1 - reclaim",
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
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
						NumberOfCacheEvictions:  1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "(This is used by the demo) 2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1 - reclaim",
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
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					}, {
						Name:                "running_job2",
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
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1 - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
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
					"node1": {
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
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"running_job2": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1, deserved of queue1 is 1 -  don't reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "running_job2",
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
					"node1": {
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
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job2": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
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
				Name: "2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1, deserved of queue1 is 1 -  don't reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
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
					"node1": {
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
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job2": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
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
				Name: "1 running job over quota but with queue that has higher reclaimable deserved gpus, other job is pending but asks the exact amount of GPUs as in deserved, cluster is in over capacity - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 8,
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
						RequiredGPUsPerTask: 5,
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
						GPUs: 8,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       6,
						GPUOverQuotaWeight: 6,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 8,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 5,
						Status:       pod_status.Running,
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
				Name: "queue0 is over quota, queue1 is under quota, yet we don't reclaim because allocate will case a loop (this is a bug in allocate)",
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
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 3,
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
					}, {
						Name:                "pending_job2",
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
					"node0": {
						GPUs: 4,
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
						GPUsRequired: 2,
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
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 3,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job2": {
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
				Name: "2 train job for overquota project 1, 1 job for overquota project with deserved of 0, 1 job recalim from different project - reclaim the job with quota of 0",
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
						QueueName:           "queue2",
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
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "2 train job for overquota project 1, 1 job for overquota project with deserved of 0, 1 job recalim from different project - reclaim from less prioritized queue",
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
						Name:                "running_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
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
						QueueName:           "queue2",
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
						GPUOverQuotaWeight: 0,
						Priority:           pointer.Int(commonconstants.DefaultQueuePriority + 1),
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
						Status:       pod_status.Pending,
					},
					"running_job2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job3": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "Reclaim according to the fairness ratio, more GPUs then sum of deserved GPUs",
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
						RequiredGPUsPerTask: 3,
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
						RequiredGPUsPerTask: 4,
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
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 4,
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
						GPUs: 8,
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
					"running_job2": {
						GPUsRequired: 4,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Running,
					},
					"pending_job1": {
						GPUsRequired: 4,
						Status:       pod_status.Pending,
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
				Name: "Reclaim according to the reclaimable deserved GPUs, queue0 has 1 GPU from remaining from setQueueReclaimableResources remaining logic",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
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
						GPUs: 7,
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
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 3,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "Classic reclaim for train jobs between departments",
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
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 1,
					},
					{
						Name:         "d2",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "Classic interactive job reclaim train job from different department",
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
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 1,
					},
					{
						Name:         "d2",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "Don't reclaim if department will be overquota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 3,
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
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
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
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d2",
					},
					{
						Name:               "queue2",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 2,
					},
					{
						Name:         "d2",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 3,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
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
				Name: "Reclaim trains according to deserved quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 4,
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
						RequiredGPUsPerTask: 4,
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
						GPUs: 8,
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
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 1,
					},
					{
						Name:         "d2",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Running,
					},
					"running_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Running,
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
				Name: "Reclaim trains according to priority",
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
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "q0_running_job3",
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
						Name:                "q0_running_job4",
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
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "reclaimer",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "reclaimer_queue",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 8,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 2,
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
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 1,
						Priority:           pointer.Int(commonconstants.DefaultQueuePriority + 1),
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
						Status:       pod_status.Running,
					},
					"q0_running_job3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"q0_running_job4": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
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
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"reclaimer": {
						NodeName:     "node0",
						GPUsRequired: 3,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim from different department and same department",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
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
						RequiredGPUsPerTask: 4,
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
						GPUs: 4,
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
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d2",
					},
					{
						Name:               "queue2",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 4,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 0,
					},
					{
						Name:         "d2",
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"running_job1": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim job when the pending job with the second highest priority job should reclaim (this was a bug)",
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
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "Verify we do only 1 round of GPU allocation for whole GPUs jobs, and give the GPUs to the one with the bigger remaining - this is a bug we had",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 5,
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
						Tasks: []*tasks_fake.TestTaskBasic{
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
						RequiredGPUsPerTask: 3,
						QueueName:           "queue1",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 8,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 4,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
						NumberOfCacheBinds:      1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 5,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
					"running_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"running_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						GPUsRequired: 3,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 queue with big quota will be satisfied with 1 gpu, give the rest of the gpus according to the last test",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 5,
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
						Name:                "pending_job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 3,
						QueueName:           "queue1",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 9,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 4,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 4,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       10,
						GPUOverQuotaWeight: 10,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
						NumberOfCacheBinds:      1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 5,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
					"running_job1": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"running_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job3": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 3,
						Status:       pod_status.Running,
						NodeName:     "node0",
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Verify we do only 1 round of GPU allocation for whole GPUs jobs - this is a bug we had",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 3,
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
						QueueName:           "queue1",
						Priority:            constants.PriorityTrainNumber,
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
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 3,
						Status:       pod_status.Running,
						NodeName:     "node0",
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
				Name: "[useOnlyFreeResources OFF sanity] q0 allocated only cpu tasks, has 0 deserved GPUs and useOnlyFreeResources=false (implicit). q1 requires cpu only tasks of same priority to be allocated and does not reclaim from q0 jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 100000000,
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
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:     2,
						NumberOfCacheEvictions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "[useOnlyFreeResources OFF sanity] q0 allocated CPU and GPU tasks, has 0 deserved GPUs and useOnlyFreeResources=false (implicit)." +
					" q1 requires cpu+gpu tasks of same priority to be allocated and reclaims the resources of the GPU job from q0." +
					" the cpu only job from q1 is scheduled because it is defined before the GPU+CPU task",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   1,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   1,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 100000000,
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
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						GPUsRequired:     1,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						GPUsRequired:     1,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
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
				Name: "[useOnlyFreeResources OFF sanity] q0,q1 allocated both gpu and cpu tasks, q1 has 0 deserved gpus and useOnlyFreeResources=false." +
					" when tasks from q2 with higher priority require cpu + gpu, it reclaims both jobs from q1 despite only 1 of them having requested GPU because 2 jobs from q2 require GPU and memory limit on node allows 4 jobs total",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   3,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   3,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job2",
						RequiredGPUsPerTask:   1,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job3",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   4,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   3,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   3,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job2",
						RequiredGPUsPerTask:   2,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      10,
						CPUMemory: 200000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
					},
					{
						Name:               "queue2",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 2000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 3000,
						Status:           pod_status.Running,
					},
					"running_job2": {
						GPUsRequired:     1,
						MilliCpuRequired: 1000,
						Status:           pod_status.Pending,
					},
					"running_job3": {
						GPUsRequired:     0,
						MilliCpuRequired: 4000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 1000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 3000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job2": {
						GPUsRequired:     2,
						MilliCpuRequired: 1000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
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
				Name: "q0 allocated only cpu tasks, has 0 deserved resources and useOnlyFreeResources=true. q1 requires cpu only tasks of same priority to be allocated and reclaims from q0 jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 100000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						Status:           pod_status.Pending,
					},
					"running_job1": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
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
				Name: "q0 allocated CPU and GPU tasks, has 0 deserved GPUs and useOnlyFreeResources=true. q1 requires cpu+gpu tasks of same priority to be allocated and reclaim from q0 jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   1,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   1,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 100000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
					"running_job1": {
						GPUsRequired:     1,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     1,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
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
				Name: "q0,q1 allocated both gpu and cpu tasks, q1 has 0 deserved resources and useOnlyFreeResources=true. when tasks from q2 with higher priority require cpu, it reclaim from q1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   2,
						RequiredMemoryPerTask: 40000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   3,
						RequiredMemoryPerTask: 40000000,
						RequiredCPUsPerTask:   3,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job2",
						RequiredGPUsPerTask:   1,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job3",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   4,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   3,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   3,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job2",
						RequiredGPUsPerTask:   2,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      10,
						CPUMemory: 200000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue2",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 2000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 3000,
						Status:           pod_status.Running,
					},
					"running_job2": {
						GPUsRequired:     1,
						MilliCpuRequired: 1000,
						Status:           pod_status.Pending,
					},
					"running_job3": {
						GPUsRequired:     0,
						MilliCpuRequired: 4000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 1000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 3000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
					"pending_job2": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 1000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "q0,q1 allocated both gpu and cpu tasks, q1 has deserved GPUs and useOnlyFreeResources=true. when tasks from q2 with higher priority require cpu and gpu, it will reclaim ALL from q1 as memory on the node is not enough for all",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   2,
						RequiredMemoryPerTask: 40000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   3,
						RequiredMemoryPerTask: 40000000,
						RequiredCPUsPerTask:   3,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job2",
						RequiredGPUsPerTask:   1,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job3",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   4,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   3,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   3,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job2",
						RequiredGPUsPerTask:   2,
						RequiredCPUsPerTask:   1,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      10,
						CPUMemory: 200000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue2",
						DeservedGPUs:       5,
						GPUOverQuotaWeight: 5,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 2000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 3000,
						Status:           pod_status.Running,
					},
					"running_job2": {
						GPUsRequired:     1,
						MilliCpuRequired: 1000,
						Status:           pod_status.Pending,
					},
					"running_job3": {
						GPUsRequired:     0,
						MilliCpuRequired: 4000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     3,
						MilliCpuRequired: 1000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 3000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
					"pending_job2": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 1000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "q0 allocated only cpu tasks, has 0 deserved resources and useOnlyFreeResources=true. q1 requires cpu only tasks of higher priority to be allocated and reclaim (memory) from q1 jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 40000000,
						RequiredCPUsPerTask:   4,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   4,
						RequiredMemoryPerTask: 40000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 40000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 4000,
						MemoryRequired:   40000000,
						Status:           pod_status.Pending,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 4000,
						MemoryRequired:   40000000,
						Status:           pod_status.Running,
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
				Name: "q0 allocated only cpu tasks, has 0 deserved resources and useOnlyFreeResources=true. q1 requires cpu only tasks of higher priority to be allocated and reclaim (CPU) from q1 jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   5,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   5,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   5,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   5,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 200000000,
						CPUMillis: 100000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
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
				Name: "q0 allocated only cpu tasks, has 0 deserved resources and useOnlyFreeResources=true. q1 requires cpu only tasks of higher priority to be allocated and does not reclaim q0 jobs because there are enough cpu and memory resources",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   2,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   0,
						RequiredCPUsPerTask:   2,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 200000000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job0": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     0,
						MilliCpuRequired: 2000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:     2,
						NumberOfCacheEvictions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "q0 allocated cpu and gpu tasks, has 0 deserved gpus and useOnlyFreeResources=true." +
					" q1 requires cpu only tasks of higher priority to be allocated and does not reclaim q0 jobs because does not deserve enough GPU",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job0",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   5,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "running_job1",
						RequiredGPUsPerTask:   0,
						RequiredMemoryPerTask: 50000000,
						RequiredCPUsPerTask:   5,
						Priority:              constants.PriorityTrainNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job0",
						RequiredGPUsPerTask:   2,
						RequiredCPUsPerTask:   5,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						RequiredGPUsPerTask:   2,
						RequiredCPUsPerTask:   5,
						RequiredMemoryPerTask: 50000000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      2,
						CPUMemory: 200000000,
						CPUMillis: 100000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
						GPUOverQuotaWeight: 0,
						DeservedCPUs:       pointer.Float64(0),
						DeservedMemory:     pointer.Float64(0),
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"running_job1": {
						GPUsRequired:     0,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired:     2,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
					"pending_job1": {
						NodeName:         "node0",
						GPUsRequired:     2,
						MilliCpuRequired: 5000,
						MemoryRequired:   50000000,
						Status:           pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "d2 is under deserved quota and d1 is over quota and within fair share - reclaim",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
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
						GPUs: 3,
					},
					"node1": {
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 3,
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 2,
					},
					{
						Name:         "d2",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
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
				Name: "both departments got deserved quota, a queue within d2 is under quota - reclaim from another queue",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
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
						GPUs: 3,
					},
					"node1": {
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       3,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d2",
					},
					{
						Name:               "queue2",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 3,
					},
					{
						Name:         "d2",
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job2": {
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
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
				Name: "departments and queues are over deserved quota and within fair share - should not reclaim",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
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
						GPUs: 3,
					},
					"node1": {
						GPUs: 3,
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
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 1,
					},
					{
						Name:         "d2",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job2": {
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job3": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "both departments are under deserved quota - should not reclaim",
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
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node1",
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
						GPUs: 1,
					},
					"node1": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d1",
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 0,
						ParentQueue:        "d2",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:         "d1",
						DeservedGPUs: 2,
					},
					{
						Name:         "d2",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
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
				Name: "Reclaim only over quota task from elastic Job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
								Priority: pointer.Int(2),
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
								Priority: pointer.Int(1),
							},
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Pending,
								Priority: pointer.Int(2),
							},
							{
								State:    pod_status.Pending,
								Priority: pointer.Int(2),
							},
							{
								State:    pod_status.Pending,
								Priority: pointer.Int(1),
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job-1": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending-job-2": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						GPUsRequired:         0.5,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						GPUsRequired:         0.5,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      2,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						GPUsRequired:         0.7,
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						NodeName:             "node1",
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
						Status:               pod_status.Pending,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						GPUsRequired:         1,
						Status:               pod_status.Running,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
						NumberOfCacheBinds:      2,
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
						Status:               pod_status.Pending,
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
						Status:               pod_status.Running,
						NodeName:             "node1",
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						GPUsRequired:         0.7,
						Status:               pod_status.Running,
						NodeName:             "node0",
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
						NumberOfCacheBinds:      2,
					},
				},
			},
		},
	}
}
