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
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleMemoryGPUAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	testsMetadata := getMemoryGPUTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getMemoryGPUTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Non preemptible job requests gpu memory over deserved quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "pending_job-0",
						RequiredGpuMemory: 40,
						Priority:          constants.PriorityBuildNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
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
					"pending_job-0": {
						Status:       pod_status.Pending,
						GPUsAccepted: 0.8,
						GPUGroups:    []string{"0"},
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
				Name: "Preemptible job requests gpu memory over queue limit",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "pending_job-0",
						RequiredGpuMemory: 40,
						Priority:          constants.PriorityTrainNumber,
						QueueName:         "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
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
						Name:           "queue0",
						DeservedGPUs:   1,
						MaxAllowedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job-0": {
						Status:       pod_status.Pending,
						GPUsAccepted: 0.8,
						GPUGroups:    []string{"0"},
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
						NodeName:     "node0",
						GPUsAccepted: 0.5,
						Status:       pod_status.Binding,
						GPUGroups:    []string{"0"},
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
				Name: "Pending job requests gpu memory while other job terminates",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "pending_job-0",
						RequiredGpuMemory:     50,
						RequiredMemoryPerTask: 1500,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                  "running_job-0",
						RequiredMemoryPerTask: 1000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								GPUGroups: []string{"0"},
								NodeName:  "node0",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 2000,
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
						Status:         pod_status.Pipelined,
						MemoryRequired: 1500,
						GPUGroups:      []string{"0"},
					},
					"running_job-0": {
						Status:         pod_status.Releasing,
						GPUGroups:      []string{"0"},
						MemoryRequired: 1000,
						NodeName:       "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pending job requests GPU memory, assigned to an already shared GPU device, memory resource cannot be allocated",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "pending_job-0",
						RequiredGpuMemory:     50,
						RequiredMemoryPerTask: 750,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                  "running_job-0",
						RequiredMemoryPerTask: 1000,
						RequiredGpuMemory:     25,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                  "running_job-1",
						RequiredMemoryPerTask: 500,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								GPUGroups: []string{"0"},
								NodeName:  "node0",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 2000,
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
						Status:         pod_status.Pipelined,
						MemoryRequired: 750,
						GPUGroups:      []string{"0"},
					},
					"running_job-0": {
						Status:         pod_status.Running,
						GPUGroups:      []string{"0"},
						MemoryRequired: 1000,
						NodeName:       "node0",
					},
					"running_job-1": {
						Status:         pod_status.Releasing,
						GPUGroups:      []string{"0"},
						MemoryRequired: 500,
						NodeName:       "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pending job requests gpu memory, new shared GPU device selected, memory cannot be allocated",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "pending_job-0",
						RequiredGpuMemory:     50,
						RequiredMemoryPerTask: 750,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Pending,
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                  "running_job-0",
						RequiredMemoryPerTask: 1000,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Running,
								GPUGroups: []string{"0"},
								NodeName:  "node0",
							},
						},
					},
					{
						Name:                  "running_job-1",
						RequiredMemoryPerTask: 500,
						Priority:              constants.PriorityBuildNumber,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								GPUGroups: []string{"0"},
								NodeName:  "node0",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:      1,
						CPUMemory: 2000,
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
						Status:         pod_status.Pipelined,
						MemoryRequired: 750,
						GPUGroups:      []string{"0"},
					},
					"running_job-0": {
						Status:         pod_status.Running,
						GPUGroups:      []string{"0"},
						MemoryRequired: 1000,
						NodeName:       "node0",
					},
					"running_job-1": {
						Status:         pod_status.Releasing,
						GPUGroups:      []string{"0"},
						MemoryRequired: 500,
						NodeName:       "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
	}
}
