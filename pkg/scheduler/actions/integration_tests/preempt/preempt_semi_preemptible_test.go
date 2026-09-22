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

// A semi-preemptible job whose gang formed on "b" and "c" is still holding one pod in "a", a subgroup
// that never reached its own minMember. That pod belongs to no gang and holds no core slot, so quota
// accounting counts it as reclaimable - this drives a real session end to end to prove eviction
// agrees, and that the preemptor actually lands on it.
func TestSemiPreemptibleOrphanEvictionIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "orphan pod in an unformed subgroup is the preemption victim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						Preemptibility:      enginev2alpha2.SemiPreemptible,
						QueueName:           "queue0",
						RootSubGroupSet: func() *subgroup_info.SubGroupSet {
							root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
							root.SetMinSubGroup(ptr.To(int32(2)))
							for _, name := range []string{"a", "b", "c"} {
								root.AddPodSet(subgroup_info.NewPodSet(name, 2, nil))
							}
							return root
						}(),
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "a"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "b"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "b"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "c"},
							{NodeName: "node0", State: pod_status.Running, SubGroupName: "c"},
						},
					},
					{
						Name:                "preemptor0",
						RequiredGPUsPerTask: 1,
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
					"job0-0":       {GPUsRequired: 1, Status: pod_status.Pending},
					"job0-1":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-2":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-3":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"job0-4":       {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
					"preemptor0-0": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Running},
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
