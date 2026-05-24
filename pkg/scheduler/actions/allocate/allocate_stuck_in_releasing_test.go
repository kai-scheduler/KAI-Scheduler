// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestHandleAllocationStuckInReleasing verifies that a Running pod with a
// long-elapsed deletionTimestamp (classified as StuckInReleasing) does not
// contribute its resources to the node's releasing pool, preventing pending
// jobs from being pipelined onto it. A control case with the same setup but
// State=Releasing confirms pipelining still works for fresh deletions.
func TestHandleAllocationStuckInReleasing(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	tests := []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Releasing victim allows pipelining of pending pod",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Releasing, NodeName: "node0"},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 1},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job": {
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
						NodeName:     "node0",
					},
					"pending_job": {
						GPUsRequired: 1,
						Status:       pod_status.Pipelined,
						NodeName:     "node0",
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "StuckInReleasing victim does not allow pipelining",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.StuckInReleasing, NodeName: "node0"},
						},
					},
					{
						Name:                "pending_job",
						RequiredGPUsPerTask: 1,
						QueueName:           "queue0",
						Priority:            constants.PriorityTrainNumber,
						Tasks: []*tasks_fake.TestTaskBasic{
							{State: pod_status.Pending},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 1},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfPipelineActions: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job": {
						GPUsRequired: 1,
						Status:       pod_status.StuckInReleasing,
						NodeName:     "node0",
					},
					"pending_job": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
	}

	for testNumber, testMetadata := range tests {
		t.Run(testMetadata.TestTopologyBasic.Name, func(t *testing.T) {
			ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
			allocate.New().Execute(ssn)
			test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
		})
	}
}
