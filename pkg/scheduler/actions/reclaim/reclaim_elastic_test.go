// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleElasticReclaim(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()
	testsMetadata := getTestsElasticMetadata()

	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		reclaimAction := reclaim.New()
		reclaimAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTestsElasticMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim single task from elastic job",
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
							},
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
								NodeName: "node0",
								State:    pod_status.Pending,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},

					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
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
				Name: "Reclaim several tasks from elastic job",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job-2": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job-3": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						GPUsRequired:         1,
						Status:               pod_status.Pending,
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
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim all tasks from elastic job",
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
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending-job",
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
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
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
				Name: "Reclaim all tasks from job and a single task from elastic job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
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
						Name:                "running-job1",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						GPUs: 3,
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
						DeservedGPUs:       3,
						GPUOverQuotaWeight: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
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
				Name: "Reclaim tasks from several elastic jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
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
						Name:                "running-job1",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						GPUs: 4,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job1-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
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
				Name: "Reclaim from several running jobs (and of them is elastic)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
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
						Name:                "running-job1",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
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
						GPUs: 3,
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
						DeservedGPUs:       3,
						GPUOverQuotaWeight: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"running-job1-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         3,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      3,
						NumberOfCacheEvictions:  3,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Elastic job reclaims another non-elastic job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         2,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						NodeName:             "node0",
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
				Name: "Elastic job reclaims single task from another elastic job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
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
						Name:                "pending-job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
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
						GPUs: 3,
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job0-1": {
						NodeName:             "node0",
						GPUsRequired:         1,
						Status:               pod_status.Releasing,
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
				Name: "Elastic job reclaims single task from another elastic job - equal queues",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job0",
						RequiredGPUsPerTask: 0,
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
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 0,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
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
						GPUs:      4,
						CPUMillis: 4000,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:           "department-a",
						MaxAllowedCPUs: test_utils.CreateFloat64Pointer(4000),
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedCPUs:       pointer.Float64(1000),
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						ParentQueue:        "department-a",
						DeservedCPUs:       pointer.Float64(1000),
						GPUOverQuotaWeight: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job0-0": {
						NodeName:             "node0",
						GPUsRequired:         0,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job0-1": {
						NodeName:             "node0",
						GPUsRequired:         0,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"running-job0-2": {
						GPUsRequired:         0,
						Status:               pod_status.Releasing,
						DontValidateGPUGroup: true,
					},
					"pending-job-0": {
						NodeName:             "node0",
						GPUsRequired:         0,
						Status:               pod_status.Running,
						DontValidateGPUGroup: true,
					},
					"pending-job-1": {
						NodeName:             "node0",
						GPUsRequired:         0,
						Status:               pod_status.Pipelined,
						DontValidateGPUGroup: true,
					},
					"pending-job-2": {
						GPUsRequired:         0,
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
				Name: "Don't reclaim elastic job which is in quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running-job-0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(2),
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
						},
					},
					{
						Name:                "pending-job",
						RequiredGPUsPerTask: 2,
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
						Name:               "queue2",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-job-0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running-job-0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running-job-0-2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending-job-0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
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
