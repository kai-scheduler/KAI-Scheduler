// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleFractionalGPUAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	testsMetadata := getFractionalGPUTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getFractionalGPUTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "allocate job with multiple pods but does not allow it because project goes over allowance",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
						MaxAllowedGPUs:     1,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:           "department-a",
						DeservedGPUs:   1,
						MaxAllowedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1.5,
						Status:       pod_status.Pending,
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
				Name: "allocate job with multiple pods but does not allow it because department to go over allowance",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						ParentQueue:        "department-a",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name:           "department-a",
						DeservedGPUs:   1,
						MaxAllowedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1.5,
						Status:       pod_status.Pending,
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
				Name: "1 shared gpu job running, 1 pending interactive shared gpu job, allocate to same gpu",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
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
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
					"pending_shared_gpu_job1": {
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
						GPUsRequired: 0.5,
						Status:       pod_status.Binding,
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
				Name: "1 shared gpu job running, 1 pending interactive shared gpu job, allocate to same gpu - even if multiple nodes, to check gpusharingorder plugin",
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
						DeservedGPUs: 2,
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
						GPUGroups:    []string{"1"},
						GPUsRequired: 0.5,
						Status:       pod_status.Binding,
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
						Status:       pod_status.Binding,
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
				Name: "1 shared gpu job running, 1 pending interactive shared gpu job - queue will be overused, don't allocate",
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
						DeservedGPUs: 1,
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
						GPUsRequired: 0.5,
						Status:       pod_status.Pending,
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
				Name: "1 shared gpu job running, 1 pending interactive shared gpu job - queue will be overused, don't allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.6,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
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
						DeservedGPUs: 1,
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
						GPUsRequired: 0.5,
						Status:       pod_status.Pending,
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
						Status:               pod_status.Binding,
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
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 shared gpu interactive job running, 1 pending whole gpu job - allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_shared_gpu_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								NodeName:  "node0",
								State:     pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_whole_gpu_job1",
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
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
					"pending_whole_gpu_job1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Binding,
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
						Status:       pod_status.Binding,
					},
					"pending_shared_gpu_job1": {
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						NodeName:     "node0",
						Status:       pod_status.Binding,
					},
					"pending_shared_gpu_job2": {
						GPUsRequired: 0.5,
						Status:       pod_status.Pending,
					},
					"pending_shared_gpu_job3": {
						GPUsRequired: 0.3,
						GPUGroups:    []string{"1"},
						NodeName:     "node0",
						Status:       pod_status.Binding,
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
				Name: "Verify allocated fractional GPU jobs are counted in the overall number of GPUs the default department will have",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
								NodeName:  "node0",
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
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pipeline fractional interactive gpu task when a task is evicting - dont allocate a new gpu",
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
						Name:                "releasing_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Releasing,
								NodeName:  "node0",
							},
						},
					}, {
						Name:                "pending_job0",
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
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Running,
					},
					"releasing_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
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
						Status:       pod_status.Binding,
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
						Status:       pod_status.Binding,
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
				Name: "test gpuSharingOrder for a node that has a gpu reservation pod pending - two pods",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pipelined_shared_gpu_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								NodeName:  "node1",
								State:     pod_status.Pipelined,
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
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pipelined_shared_gpu_job0": {
						NodeName:     "node1",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Pipelined,
					},
					"pending_shared_gpu_job1": {
						NodeName:     "node1",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Spread across nodes, when 1 node has shared GPU",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.2,
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
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						GPUGroups:    []string{"0"},
						NodeName:     "node0",
					},
					"pending_job0": {
						GPUsRequired: 0.2,
						GPUGroups:    []string{"0"},
						Status:       pod_status.Binding,
						NodeName:     "node1",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "nodeplacement",
										Arguments: map[string]string{
											constants.GPUResource: constants.SpreadStrategy,
											constants.CPUResource: constants.SpreadStrategy,
										},
									},
									{
										Name: "gpuspread",
									},
									{
										Name: "proportion",
									},
									{
										Name: "priority",
									},
									{
										Name: "nodeavailability",
									},
									{
										Name: "resourcetype",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pack jobs on nodes",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.2,
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
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.2,
						Status:       pod_status.Binding,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pack jobs on GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.7,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.2,
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
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
					"running_job1": {
						GPUsRequired: 0.7,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.2,
						Status:       pod_status.Binding,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "gpupack",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Spread jobs across GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.7,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.2,
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
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
					"running_job1": {
						GPUsRequired: 0.7,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.2,
						Status:       pod_status.Binding,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "gpuspread",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Spread jobs across free GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"1"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "running_job1",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.7,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.2,
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
						Name:         "queue0",
						DeservedGPUs: 8,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
					"running_job1": {
						GPUsRequired: 0.7,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.2,
						Status:       pod_status.Binding,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "gpuspread",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pack nodes, spread GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
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
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Binding,
						NodeName:     "node0",
						GPUGroups:    []string{"1"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "gpuspread",
									},
									{
										Name: "nodeplacement",
										Arguments: map[string]string{
											constants.GPUResource: constants.BinpackStrategy,
											constants.CPUResource: constants.BinpackStrategy,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Spread nodes, pack GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								GPUGroups: []string{"0"},
								State:     pod_status.Running,
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job1",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 0.5,
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
						GPUs: 3,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUGroups:    []string{"0"},
					},
					"pending_job0": {
						GPUsRequired: 0.5,
						Status:       pod_status.Binding,
						NodeName:     "node1",
						GPUGroups:    []string{"0"},
					},
					"pending_job1": {
						GPUsRequired: 0.5,
						Status:       pod_status.Binding,
						NodeName:     "node1",
						GPUGroups:    []string{"0"},
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "nodeplacement",
										Arguments: map[string]string{
											constants.GPUResource: constants.SpreadStrategy,
											constants.CPUResource: constants.SpreadStrategy,
										},
									},
									{
										Name: "gpupack",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
