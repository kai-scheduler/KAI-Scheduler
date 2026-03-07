// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestSubGroupsConsolidation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getSubGroupsConsolidationTestsMetadata()

	for testNumber, testMetadata := range testsMetadata {
		fmt.Printf("Running test %d/%d: %s\n", testNumber, len(testsMetadata), testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		consolidationAction := consolidation.New()
		consolidationAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getSubGroupsConsolidationTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "job with sub groups consolidates a running job",
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
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:      "pending_job0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
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
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(3)),
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
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
					"pending_job0": {
						GPUsRequired: 4,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Job with sub groups consolidates another job up to MinAvailable",
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
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:      "pending_job0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
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
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(2)),
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0-0": {
						GPUsRequired: 3,
						NodeName:     "node0",
						Status:       pod_status.Running,
					},
					"running_job1-0": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Pipelined,
					},
					"pending_job0-0": {
						GPUsRequired: 2,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
					},
					"pending_job0-1": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0-2": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0-3": {
						GPUsRequired: 2,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
					},
					"pending_job0-4": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job0-5": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 3,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "job with sub groups cannot consolidate a running job - cannot satisfy sub group gang",
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
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:      "pending_job0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
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
								RequiredGPUs: ptr.To(int64(3)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(3)),
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
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node1",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 6,
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
				Name: "job with sub groups consolidated by a pending job - partial eviction",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Running,
								NodeName:     "node0",
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Running,
								NodeName:     "node1",
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(3)),
							},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 4,
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
					"node1": {
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
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job-1": {
						NodeName:     "node0",
						GPUsRequired: 3,
						Status:       pod_status.Pipelined,
					},
					"pending_job-0": {
						GPUsRequired: 4,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
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
				Name: "job with sub groups consolidated by a pending job - complete eviction",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Running,
								NodeName:     "node0",
								RequiredGPUs: ptr.To(int64(2)),
							},
						},
					},
					{
						Name:      "running_job1",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub-1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Running,
								NodeName:     "node1",
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Running,
								NodeName:     "node2",
								SubGroupName: "sub-1",
								RequiredGPUs: ptr.To(int64(1)),
							},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
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
						GPUs: 4,
					},
					"node1": {
						GPUs: 4,
					},
					"node2": {
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
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"running_job1-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job-0": {
						GPUsRequired: 4,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
					},
					"pending_job-1": {
						GPUsRequired: 4,
						NodeName:     "node2",
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 4,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "job with sub groups above minAvailable consolidated by a pending job - complete eviction",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub-0", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Running,
								NodeName:     "node0",
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Running,
								NodeName:     "node1",
								SubGroupName: "sub-0",
								RequiredGPUs: ptr.To(int64(1)),
							},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 4,
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
					"node1": {
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
					"running_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
					},
					"pending_job-0": {
						GPUsRequired: 4,
						NodeName:     "node1",
						Status:       pod_status.Pipelined,
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
	}
}
