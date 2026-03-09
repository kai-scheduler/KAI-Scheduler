// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	"k8s.io/utils/ptr"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleSubGroupsAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getAllocationSubGroupsTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getAllocationSubGroupsTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - full allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
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
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - partial allocation",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
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
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0-2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - allocate resource to unsatisfied sub group",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 2, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 2, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-3": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-4": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - allocate resource up to MinAvailable",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 2, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(2)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 3,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-4": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-5": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-6": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - allocate resource beyond MinAvailable",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 2, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:     "node0",
								State:        pod_status.Running,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 3,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-4": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"job0-5": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-6": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - unbalanced hierarchy structure",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)

							subAb := subgroup_info.NewSubGroupSet("sub-ab", nil)
							root.AddSubGroup(subAb)

							subAb.AddPodSet(subgroup_info.NewPodSet("sub-a", 2, nil))
							subAb.AddPodSet(subgroup_info.NewPodSet("sub-b", 2, nil))

							root.AddPodSet(subgroup_info.NewPodSet("sub-c", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-a",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-a",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-b",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-b",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub-c",
								RequiredGPUs: ptr.To(int64(4)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-3": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"job0-4": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Binding,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate job with SubGroups - cannot satisfy sub group gang",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 2, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(3)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(3)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						GPUsRequired: 3,
						Status:       pod_status.Pending,
					},
					"pending_job0-1": {
						GPUsRequired: 3,
						Status:       pod_status.Pending,
					},
					"pending_job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate multiple jobs with SubGroups",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
						},
					},
					{
						Name:      "pending_job1",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub0",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
							},
							{
								State:        pod_status.Pending,
								SubGroupName: "sub1",
								RequiredGPUs: ptr.To(int64(1)),
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
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 4,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job0-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job0-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1-0": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job1-1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1-2": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job1-3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
	}
}
