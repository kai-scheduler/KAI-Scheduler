// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	"gopkg.in/h2non/gock.v1"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestMIGReclaim(t *testing.T) {
	defer gock.Off()

	integration_tests_utils.RunTests(t, getMIGTestsMetadata())
}

func getMIGTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Simple reclaim with MIG jobs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job-0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 1,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job-0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 2,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 2,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						Status: pod_status.Pending,
					},
					"pending_job-0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfCacheBinds:      1,
						NumberOfPipelineActions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Should not reclaim jobs if job is within fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job-0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 2,
								},
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					},
					{
						Name:      "pending_job-0",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.10gb": 3,
								},
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.10gb": 4,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job-0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job-0": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
		},
	}
}
