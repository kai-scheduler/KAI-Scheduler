// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package stalegangeviction_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/stalegangeviction"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestStaleGangEviction(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	type testMetadata struct {
		name     string
		topology test_utils.TestTopologyBasic
	}

	for i, test := range []testMetadata{
		{
			name: "sanity",
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						QueueName:    "q-1",
						MinAvailable: pointer.Int32(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:     "job-1-0",
								State:    pod_status.Running,
								NodeName: "node-1",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:        "q-1",
						ParentQueue: "d-1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name: "d-1",
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-1-0": {
						NodeName: "node-1",
						Status:   pod_status.Running,
					},
				},
				ExpectedNodesResources: nil,
				Mocks:                  nil,
			},
		},
		{
			name: "Don't evict recently stale job",
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						QueueName:    "q-1",
						MinAvailable: pointer.Int32(2),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:     "job-1-0",
								State:    pod_status.Running,
								NodeName: "node-1",
							},
							{
								Name:     "job-1-1",
								State:    pod_status.Failed,
								NodeName: "node-1",
							},
						},
						StaleDuration: pointer.Duration(1 * time.Second),
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:        "q-1",
						ParentQueue: "d-1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name: "d-1",
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-1-0": {
						NodeName: "node-1",
						Status:   pod_status.Running,
					},
					"job-1-1": {
						NodeName: "node-1",
						Status:   pod_status.Failed,
					},
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
		{
			name: "Evict long-ago stale job",
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						QueueName:    "q-1",
						MinAvailable: pointer.Int32(2),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:     "job-1-0",
								State:    pod_status.Running,
								NodeName: "node-1",
							},
							{
								Name:     "job-1-1",
								State:    pod_status.Failed,
								NodeName: "node-1",
							},
						},
						StaleDuration: pointer.Duration(61 * time.Second),
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:        "q-1",
						ParentQueue: "d-1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name: "d-1",
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-1-0": {
						NodeName: "node-1",
						Status:   pod_status.Releasing,
					},
					"job-1-1": {
						NodeName: "node-1",
						Status:   pod_status.Failed,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      0,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 0,
					},
				},
			},
		},
		{
			name: "Don't evict non-stale job (has enough minAvailable, even though it has stale duration)",
			topology: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:         "job-1",
						QueueName:    "q-1",
						MinAvailable: pointer.Int32(1),
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								Name:     "job-1-0",
								State:    pod_status.Running,
								NodeName: "node-1",
							},
							{
								Name:     "job-1-1",
								State:    pod_status.Failed,
								NodeName: "node-1",
							},
						},
						StaleDuration: pointer.Duration(1 * time.Second),
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node-1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:        "q-1",
						ParentQueue: "d-1",
					},
				},
				Departments: []test_utils.TestDepartmentBasic{
					{
						Name: "d-1",
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-1-0": {
						NodeName: "node-1",
						Status:   pod_status.Running,
					},
					"job-1-1": {
						NodeName: "node-1",
						Status:   pod_status.Failed,
					},
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
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test number: %v, test name: %v,", i, test.name)
			ssn := test_utils.BuildSession(test.topology, controller)
			ssn.OverrideGlobalDefaultStalenessGracePeriod(60 * time.Second)

			gangEviction := stalegangeviction.New()
			gangEviction.Execute(ssn)

			test_utils.MatchExpectedAndRealTasks(t, i, test.topology, ssn)
		})
	}
}
