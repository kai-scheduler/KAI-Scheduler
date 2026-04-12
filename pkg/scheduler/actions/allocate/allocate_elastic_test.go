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

func TestHandleElasticSubGroupsAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getElasticSubGroupsTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getElasticSubGroupsTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				// All PodSets are satisfied. group-cd (minSubGroup=nil, ratio 2/2=1.0) is ordered
				// before group-ab (minSubGroup=1, ratio 2/1=2.0). Since all PodSets are satisfied,
				// getMaxNumSubGroupsToAllocate returns 1 per iteration.
				//
				// Round 1: sub-b and sub-d have no pending and are skipped; sub-c (ratio 1.0) wins
				// the slot over sub-a (ratio 2.0) → job0-6 bound.
				//
				// Round 2 (job re-queued, HasTasksToAllocate still true): sub-c's ratio rises to 2.0
				// (1 running + 1 binding). sub-a and sub-c are tied at 2.0; alphabetic tiebreaker
				// ("sub-a" < "sub-c") gives sub-a the slot → job0-2 bound.
				//
				// Round 3: node is full (7/7 GPUs), allocation fails — job is not re-queued.
				Name: "Elastic allocation: satisfied PodSets in two-parent tree, lower-ratio group wins first extra slot then alphabetic tiebreak allocates second",
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
				// 5 GPUs used by running tasks; 2 free — enough for 2 extra tasks, but the
				// elastic limit (maxNumSubGroupsToAllocate=1 when all satisfied) allows only 1.
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
					// sub-a's first extra task wins the 2nd elastic slot (tied ratio 2.0, "sub-a" < "sub-c")
					"job0-2": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Binding},
					"job0-3": {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-4": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-5": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					// sub-c's first extra task wins the 1st elastic slot (group-cd ratio 1.0 < group-ab ratio 2.0)
					"job0-6": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Binding},
					"job0-7": {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-8": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
				},
			},
		},
	}
}
