// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	"k8s.io/utils/ptr"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// minNonPreemptible raises the protected shape above the gang minimum: this job is schedulable on a
// single subgroup (minSubGroup=1) but protects two, so preemption may take the other two and no more.
func TestSemiPreemptibleMinNonPreemptibleIntegrationTest(t *testing.T) {
	// 4 single-pod leaf subgroups, minSubGroup=1, minNonPreemptible=2.
	burstingTree := func() *subgroup_info.SubGroupSet {
		root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		root.SetMinSubGroup(ptr.To(int32(1)))
		root.SetMinNonPreemptible(ptr.To(int32(2)))
		for _, name := range []string{"a", "b", "c", "d"} {
			root.AddPodSet(subgroup_info.NewPodSet(name, 1, nil))
		}
		return root
	}
	// Rebuilt per test: the harness mutates the task structs, so sharing them across table entries
	// would leak the first test's placement into the second.
	burstingTasks := func() []*tasks_fake.TestTaskBasic {
		return []*tasks_fake.TestTaskBasic{
			{NodeName: "node0", State: pod_status.Running, SubGroupName: "a"},
			{NodeName: "node0", State: pod_status.Running, SubGroupName: "b"},
			{NodeName: "node0", State: pod_status.Running, SubGroupName: "c"},
			{NodeName: "node0", State: pod_status.Running, SubGroupName: "d"},
		}
	}

	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "shrinks to minNonPreemptible, giving up both elastic subgroups",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						Preemptibility:      enginev2alpha2.SemiPreemptible,
						QueueName:           "queue0",
						RootSubGroupSet:     burstingTree(),
						Tasks:               burstingTasks(),
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
					"job0-1":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-2":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-3":       {GPUsRequired: 1, Status: pod_status.Pending},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
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
				// The core is off limits even when nothing else will satisfy the preemptor. Under the
				// gang minimum alone a third subgroup would be evictable and this would succeed.
				Name: "refuses to give up a core subgroup even when the preemptor cannot fit otherwise",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						Preemptibility:      enginev2alpha2.SemiPreemptible,
						QueueName:           "queue0",
						RootSubGroupSet:     burstingTree(),
						Tasks:               burstingTasks(),
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 3,
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
					"job0-1":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-2":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-3":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"preemptor0-0": {GPUsRequired: 3, Status: pod_status.Pending},
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
	})
}
