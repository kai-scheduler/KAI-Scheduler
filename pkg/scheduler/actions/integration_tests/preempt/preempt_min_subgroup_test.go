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

// The solver asks IsGangSatisfied whether preempting actually solved the pending job. A minSubGroup
// job is solved once that many subgroups are satisfied, so freeing one GPU is enough here even though
// sub1 stays pending - the single node can never hold both.
func TestPreemptMinSubGroupSolvedIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "minSubGroup pending job is solved by preempting for one subgroup",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "pending0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.SetMinSubGroup(ptr.To(int32(1)))
							root.AddPodSet(subgroup_info.NewPodSet("sub0", 1, nil))
							root.AddPodSet(subgroup_info.NewPodSet("sub1", 1, nil))
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending, SubGroupName: "sub0"},
							{State: pod_status.Pending, SubGroupName: "sub1"},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 1},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim0-0":  {GPUsRequired: 1, Status: pod_status.Pending},
					"pending0-0": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"pending0-1": {GPUsRequired: 1, Status: pod_status.Pending},
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
	})
}
