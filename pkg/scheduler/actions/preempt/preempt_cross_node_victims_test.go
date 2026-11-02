// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestPreemptDoesNotEvictVictimsThatFitOriginalNode covers #2426 with the
// maintainer-preferred approach: after the preemptor is placed, victim
// reallocation prefers each victim's original node. Victims that still fit
// there are unevicted instead of being pipelined onto another node and then
// deleted on commit.
func TestPreemptDoesNotEvictVictimsThatFitOriginalNode(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	topo := test_utils.TestTopologyBasic{
		Name: "pinned preemptor must not evict victims that still fit their original node",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "elastic-victim",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{NodeName: "node0", State: pod_status.Running},
					{NodeName: "node1", State: pod_status.Running},
				},
			},
			{
				Name:                "preemptor",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityInferenceNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:             pod_status.Pending,
						NodeAffinityNames: []string{"node0"},
					},
				},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1},
			"node1": {GPUs: 1},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 2},
		},
		TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"elastic-victim-0": {
				NodeName:     "node0",
				GPUsRequired: 1,
				Status:       pod_status.Releasing,
			},
			"elastic-victim-1": {
				NodeName:     "node1",
				GPUsRequired: 1,
				Status:       pod_status.Running,
			},
			"preemptor": {
				NodeName:     "node0",
				GPUsRequired: 1,
				Status:       pod_status.Pipelined,
			},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  1,
				NumberOfPipelineActions: 1,
			},
		},
	}

	ssn := test_utils.BuildSession(topo, controller)
	// Production preempt/reclaim default: simulation pipelines of victims are
	// not consolidating moves, so a wrong cross-node pipeline would become a
	// real eviction on commit.
	ssn.OverrideAllowConsolidatingReclaim(false)

	preempt.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, topo, ssn)
}
