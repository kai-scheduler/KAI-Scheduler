// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandlePreemptSubGroups(t *testing.T) {

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getPreemptSubGroupsTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running Test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getPreemptSubGroupsTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Job with sub groups preempts low priority job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"running_job-1": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job-0": {
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Job with sub groups cannot preempts low priority job - cannot satisfy sub group gang",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						Name:                "pending_job",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job-0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job-1": {
						GPUsRequired: 2,
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
				Name: "Job with sub groups preempts low priority job - partial allocation of the pending job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"pending_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job-2": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Job with sub groups preempts low priority job - partial allocation of the pending job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
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
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"pending_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job-2": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preempt job with sub groups - partial eviction",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-0",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-0",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-1",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-1",
							},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 2,
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
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"running_job-1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-2": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"running_job-3": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"pending_job-0": {
						GPUsRequired: 2,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preempt job with sub groups - complete eviction",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 2, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 2, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-0",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-0",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-1",
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub-1",
							},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 2,
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
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-2": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"running_job-3": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Releasing,
					},
					"pending_job-0": {
						GPUsRequired: 2,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  4,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
	}
}
