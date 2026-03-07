// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deletion_tests

import (
	"testing"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
)

func TestDeletionIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getDeletionTestsMetadata())
}

func getDeletionTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "delete 1 fractional job from node",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "releasing_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
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
						Name:               "queue0",
						ParentQueue:        "default",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
						MaxAllowedGPUs:     2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
				},
				ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
					"node0": {
						ReleasingGPUs: 1,
						IdleGPUs:      1,
					},
				},
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 1,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "delete 2 fractional jobs from same GPU",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "releasing_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
							},
						},
					},
					{
						Name:                "releasing_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
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
						Name:               "queue0",
						ParentQueue:        "default",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
						MaxAllowedGPUs:     2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
					"releasing_job1": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
				},
				ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
					"node0": {
						ReleasingGPUs: 1,
						IdleGPUs:      1,
					},
				},
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 2,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "delete 2 fractional jobs from different GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "releasing_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"0"},
							},
						},
					},
					{
						Name:                "releasing_job1",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
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
						Name:               "queue0",
						ParentQueue:        "default",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
						MaxAllowedGPUs:     2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"0"},
					},
					"releasing_job1": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
				},
				ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
					"node0": {
						ReleasingGPUs: 2,
						IdleGPUs:      0,
					},
				},
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 2,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "delete 1 fractional job from same GPU as a different running fractional job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "releasing_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Releasing,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
							},
						},
					},
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 0.5,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:     pod_status.Running,
								NodeName:  "node0",
								GPUGroups: []string{"1"},
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
						Name:               "queue0",
						ParentQueue:        "default",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
						MaxAllowedGPUs:     2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {
						Status:       pod_status.Releasing,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
					"running_job0": {
						Status:       pod_status.Running,
						NodeName:     "node0",
						GPUsRequired: 0.5,
						GPUGroups:    []string{"1"},
					},
				},
				ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
					"node0": {
						ReleasingGPUs: 0,
						IdleGPUs:      1,
					},
				},
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 2,
		},
	}
}
