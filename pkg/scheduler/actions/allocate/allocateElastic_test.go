// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	"k8s.io/utils/pointer"
	"k8s.io/utils/ptr"

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

func TestHandleElasticAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getElasticTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getElasticTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate elastic job - full allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
				Name: "Allocate elastic job - partial allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
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
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate elastic job - partial allocate - with priority",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Pending,
								Priority: pointer.Int(1),
							},
							{
								State:    pod_status.Pending,
								Priority: pointer.Int(2),
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
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job0-1": {
						GPUsRequired: 1,
						NodeName:     "node0",
						Status:       pod_status.Binding,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate elastic job - full allocate - partially running",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
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
						Status:       pod_status.Binding,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate elastic job - partial allocate - some pods already running",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
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
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0-0": {
						NodeName:                    "node0",
						GPUsRequired:                1,
						Status:                      pod_status.Running,
						LastStartTimestampOlderThan: pointer.Duration(time.Second * 30),
					},
					"pending_job0-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job0-2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate 2 elastic jobs - full and partial allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
					{
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 5,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
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
					"pending_job0-2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1-2": {
						Status:       pod_status.Pending,
						GPUsRequired: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate 2 elastic jobs - both partial allocate",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
						},
					},
					{
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
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
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 5,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
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
					"pending_job0-2": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job0-3": {
						Status:       pod_status.Pending,
						GPUsRequired: 1,
					},
					"pending_job1-0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1-1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1-2": {
						Status:       pod_status.Pending,
						GPUsRequired: 1,
					},
					"pending_job1-3": {
						Status:       pod_status.Pending,
						GPUsRequired: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// All PodSets are satisfied. group-cd (minSubGroup=nil, ratio 2/2=1.0) is ordered
				// before group-ab (minSubGroup=1, ratio 2/1=2.0) at the root level.
				//
				// Round 1: root elastic fallback selects group-cd (lower ratio 1.0). Within group-cd,
				// sub-d has no pending; sub-c wins → job0-6 bound.
				//
				// Round 2 (job re-queued, HasTasksToAllocate still true): group-cd ratio stays at 1.0
				// (2/2 satisfied), still wins over group-ab (ratio 2.0). Within group-cd, sub-d (no
				// pending) skipped; sub-c wins again → job0-7 bound.
				//
				// Round 3: node is full (7/7 GPUs), allocation fails — job is not re-queued.
				Name: "Elastic allocation: satisfied PodSets in two-parent tree, lower-ratio group wins consecutive extra slots",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "job0",
						QueueName: "queue0",
						Priority:  constants.PriorityTrainNumber,
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)

							// group-ab: minSubGroup=1 → satisfied with 1 of 2 children.
							// Both children are satisfied → GetNumActiveAllocatedDirectSubGroups=2,
							// GetMinChildrenToSatisfy=1 → SubGroupSet satisfaction ratio = 2.0 (high, deprioritized).
							groupAB := subgroup_info.NewSubGroupSet("group-ab", nil)
							groupAB.SetMinSubGroup(ptr.To(int32(1)))
							groupAB.AddPodSet(subgroup_info.NewPodSet("sub-a", 1, nil))
							groupAB.AddPodSet(subgroup_info.NewPodSet("sub-b", 1, nil))
							root.AddSubGroup(groupAB)

							// group-cd: minSubGroup=nil → needs both children.
							// Both children are satisfied → GetNumActiveAllocatedDirectSubGroups=2,
							// GetMinChildrenToSatisfy=2 → SubGroupSet satisfaction ratio = 1.0 (low, prioritized first).
							groupCD := subgroup_info.NewSubGroupSet("group-cd", nil)
							groupCD.AddPodSet(subgroup_info.NewPodSet("sub-c", 1, nil))
							groupCD.AddPodSet(subgroup_info.NewPodSet("sub-d", 1, nil))
							root.AddSubGroup(groupCD)

							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							// sub-a: 2 running (ratio 2/1=2.0), 2 extra pending
							{Name: "job0-0", NodeName: "node0", State: pod_status.Running, SubGroupName: "sub-a", RequiredGPUs: ptr.To(int64(1))},
							{Name: "job0-1", NodeName: "node0", State: pod_status.Running, SubGroupName: "sub-a", RequiredGPUs: ptr.To(int64(1))},
							{Name: "job0-2", State: pod_status.Pending, SubGroupName: "sub-a", RequiredGPUs: ptr.To(int64(1))},
							{Name: "job0-3", State: pod_status.Pending, SubGroupName: "sub-a", RequiredGPUs: ptr.To(int64(1))},
							// sub-b: 1 running (ratio 1/1=1.0), no extra pending — skipped in elastic selection
							{Name: "job0-4", NodeName: "node0", State: pod_status.Running, SubGroupName: "sub-b", RequiredGPUs: ptr.To(int64(1))},
							// sub-c: 1 running (ratio 1/1=1.0), 2 extra pending — wins the elastic slot
							{Name: "job0-5", NodeName: "node0", State: pod_status.Running, SubGroupName: "sub-c", RequiredGPUs: ptr.To(int64(1))},
							{Name: "job0-6", State: pod_status.Pending, SubGroupName: "sub-c", RequiredGPUs: ptr.To(int64(1))},
							{Name: "job0-7", State: pod_status.Pending, SubGroupName: "sub-c", RequiredGPUs: ptr.To(int64(1))},
							// sub-d: 1 running (ratio 1/1=1.0), no extra pending — skipped in elastic selection
							{Name: "job0-8", NodeName: "node0", State: pod_status.Running, SubGroupName: "sub-d", RequiredGPUs: ptr.To(int64(1))},
						},
					},
				},
				// 5 GPUs used by running tasks; 2 free — enough for 2 extra tasks.
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 7},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 1},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-1": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-2": {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3": {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-4": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-5": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					// group-cd (ratio 1.0) wins over group-ab (ratio 2.0) in both rounds; sub-c gets both extra slots
					"job0-6": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Binding},
					"job0-7": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Binding},
					"job0-8": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
				},
			},
		},
	}
}
