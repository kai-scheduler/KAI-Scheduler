// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

/*
The tests in this file are written to test that allocate and deallocate actions work correctly together.
allocate_deallocate_test.go with 3 scenarios where the same job (job0) is both allocated (pending tasks placed) in the allocation step and evicted in the same scheduling cycle.
Design pattern: Each test uses a higher-priority preemptor0 (build priority, 2 GPUs/task) that can't be gang-allocated by the allocate action (only 1 free GPU), allowing job0's pending task to be allocated first. Then the preempt action evicts job0's elastic surplus for preemptor0.

The 3 scenarios:

Flat elastic PodSets -
job0 has sub0 (min=1, 3 running with elastic surplus) and sub1 (min=1, 1 pending).
The allocate action places sub1's pending task, then preempt evicts 2 elastic tasks from sub0 for a higher-priority preemptor needing 2 GPUs.

Nested SubGroupSets with elastic -
Same pattern but with inner-a/inner-b SubGroupSets wrapping the PodSets, testing eviction resolution through nested tree traversal.

Multiple elastic PodSets across sibling branches -
job0 has 3 PodSets (sub0, sub1, sub2), two with elastic surplus and one pending.
Eviction draws 1 task from each elastic PodSet (reversed order: sub1 then sub0), while sub2's pending task gets allocated.

Full subgroup allocate then evict -
job0 has two branches: group-a wraps ps-target (min=2, 2 pending) and group-b wraps ps-other (min=1, 2 running).
Allocate places both ps-target pods. Preempt then evicts all 4 job0 tasks (elastic from ps-other, then gang eviction of the remainder)
to free 4 GPUs for the preemptor. ps-target experiences a full allocate-then-evict cycle for all its pods.

MinSubGroup elastic full preemption -
job0 has root minSubGroup=1, two PodSets (sub0, sub1) each with min=1 and 2 running tasks (1 elastic surplus each).
The preemptor needs all 4 GPUs, so it progressively evicts the elastic surplus, then gang-evicts the remaining subgroups
until the entire job is preempted.
*/
func TestAllocateDeallocateSubgroupsIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getAllocateDeallocateSubgroupsTestsMetadata())
}

func getAllocateDeallocateSubgroupsTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Elastic eviction from PodSet above minAvailable, allocate pending in same job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{State: pod_status.Pending, SubGroupName: "sub1"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 4},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-1":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-2":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  5,
						NumberOfPipelineActions: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Nested SubGroupSets - elastic eviction from deep branch, allocate in sibling of same job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							innerA := subgroup_info.NewSubGroupSet("inner-a", nil)
							innerA.AddPodSet(subgroup_info.NewPodSet("ps-a", 1, nil))
							root.AddSubGroup(innerA)
							innerB := subgroup_info.NewSubGroupSet("inner-b", nil)
							innerB.AddPodSet(subgroup_info.NewPodSet("ps-b", 1, nil))
							root.AddSubGroup(innerB)
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "ps-a"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "ps-a"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "ps-a"},
							{State: pod_status.Pending, SubGroupName: "ps-b"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 4},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-1":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-2":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  5,
						NumberOfPipelineActions: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Multiple elastic PodSets in sibling branches - evict from both, allocate pending in same job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub2", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub1"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub1"},
							{State: pod_status.Pending, SubGroupName: "sub2"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 5},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 5},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-1":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-2":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-3":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-4":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      5,
						NumberOfCacheEvictions:  5,
						NumberOfPipelineActions: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Full subgroup allocate then evict - all pods in ps-target allocated and then evicted",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							groupA := subgroup_info.NewSubGroupSet("group-a", nil)
							groupA.AddPodSet(subgroup_info.NewPodSet("ps-target", 2, nil))
							root.AddSubGroup(groupA)
							groupB := subgroup_info.NewSubGroupSet("group-b", nil)
							groupB.AddPodSet(subgroup_info.NewPodSet("ps-other", 1, nil))
							root.AddSubGroup(groupB)
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending, SubGroupName: "ps-target"},
							{State: pod_status.Pending, SubGroupName: "ps-target"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "ps-other"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "ps-other"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 4},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-1":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-2":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3":       {GPUsRequired: 1, Status: pod_status.Pending},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      8,
						NumberOfCacheEvictions:  8,
						NumberOfPipelineActions: 8,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "MinSubGroup elastic full preemption - preemptor evicts entire job with minSubGroup=1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.SetMinSubGroup(ptr.To(int32(1)))
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub0"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub1"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "sub1"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 4},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job0-0":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-1":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-2":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3":       {GPUsRequired: 1, Status: pod_status.Pending},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      8,
						NumberOfCacheEvictions:  8,
						NumberOfPipelineActions: 8,
					},
				},
			},
		},
	}
}
