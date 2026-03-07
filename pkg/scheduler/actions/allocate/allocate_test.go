/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getTestsMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Fraction job on MIG node",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 0.1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:        2,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 0.1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 job running on node0 from queue0, 3 pending jobs from queue1 and 1 pending job from queue0 - allocate them according to their the queue shares",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job2",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job3",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
					"node1": {
						GPUs: 2,
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
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job2": {
						NodeName:     "node1",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job3": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 job running on node0, 3 pending jobs - allocate them according to their order",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
					"node1": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node1",
						GPUsRequired: 4,
						Status:       pod_status.Binding,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Binding,
					},
					"pending_job2": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 job running on node0, 3 pending jobs - only build job should be allocated",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 4,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job2": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 job running on node0, 3 pending jobs, not enough deserved GPUs for build job - allocate train job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 4,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job2",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 4,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Binding,
					},
					"pending_job2": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 build job running on 2 GPU, assign pending jobs of best effort",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:            "pending_job0",
						Priority:        constants.PriorityTrainNumber,
						QueueName:       "queue1",
						IsBestEffortJob: true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:            "pending_job1",
						Priority:        constants.PriorityTrainNumber,
						QueueName:       "queue1",
						IsBestEffortJob: true,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
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
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job1": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Both queues have deserved of 2, overall 4 gpus, queue0 will pass the deserved if its pending jobs will be allocated - allocate a job from queue1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 has deserved of 3, queue 1 has deserved of 2, they will both have a share of 1 if allocated their task, yet for fair convergence - queue1 should allocate the task",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Both queues has the same share, give the one with smaller job - allocate jobs queue1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 4,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Both queues has exactly the same share - give priority to the created 1st (queue0)",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node0",
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 has no deserved GPUs, queue1 has deserved - allocate jobs queue1",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "No jobs",
				Jobs: []*jobs_fake.TestJobBasic{},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "No nodes",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                "pending_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 running jobs and 1 pending, allocate preemptible interactive with over quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "running_job1",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"running_job1": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 running jobs and 1 build pending, allocate build interactive with over quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 5,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 5,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Do not allocate regular GPU task on dynamic MIG node",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:        1,
						MigStrategy: "mixed",
						GPUName:     "A100-40GB",
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 0,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate task according to nodepack - to the node with the least free GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
						NodeName:     "node1",
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node1",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate task according to nodespread - to the node with the most free GPUs",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:    pod_status.Running,
								NodeName: "node1",
							},
						},
					},
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
					"node1": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
						NodeName:     "node1",
					},
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
					SchedulerConf: &conf.SchedulerConfiguration{
						Actions: "allocate",
						Tiers: []conf.Tier{
							{
								Plugins: []conf.PluginOption{
									{
										Name: "nodeplacement",
										Arguments: map[string]string{
											constants.GPUResource: constants.SpreadStrategy,
											constants.CPUResource: constants.SpreadStrategy,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate inference before build",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job1",
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job2",
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Allocate inference before train",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job1",
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
					{
						Name:                "pending_job2",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"pending_job1": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0",
					},
					"pending_job2": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Attempt to allocate inference over quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						Priority:            constants.PriorityInferenceNumber,
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 0,
					},
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
						NodeName:     "",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DRF - over quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "pending_job0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   900,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job1",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   900,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job2",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   900,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "oq_job0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job3",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 900 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job4",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 900 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "pending_job5",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 900 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "oq_job1",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   100,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMemory: 3000 * 10e6,
						CPUMillis: 3000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
					{
						Name:           "queue1",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"oq_job0": {
						Status: pod_status.Pending,
					},
					"oq_job1": {
						Status: pod_status.Pending,
					},
					"pending_job0": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job1": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job2": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job3": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job4": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
					"pending_job5": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 6,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DRF - order",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                  "running_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   1000,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_a_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   700,
						QueueName:             "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					}, {
						Name:                  "running_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 900 * 10e6,
						RequiredCPUsPerTask:   800,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                  "pending_job_b_0",
						Priority:              constants.PriorityTrainNumber,
						RequiredMemoryPerTask: 100 * 10e6,
						RequiredCPUsPerTask:   700,
						QueueName:             "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMemory: 3000 * 10e6,
						CPUMillis: 3000,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
					{
						Name:           "queue1",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(1500),
						DeservedMemory: test_utils.CreateFloat64Pointer(1500),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job_a_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_job_a_0": {
						Status: pod_status.Pending,
					},
					"running_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_job_b_0": {
						NodeName: "node0",
						Status:   pod_status.Binding,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
	}
}

func TestHandleElasticJobCommitFailure(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	ssn := test_utils.BuildSession(
		test_utils.TestTopologyBasic{
			Name: "elastic job",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "pending_job",
					RequiredGPUsPerTask: 1,
					Priority:            constants.PriorityTrainNumber,
					QueueName:           "queue1",
					RootSubGroupSet:     jobs_fake.DefaultSubGroup(1),
					Tasks: []*tasks_fake.TestTaskBasic{
						{
							State: pod_status.Pending,
						},
						{
							State: pod_status.Pending,
						},
					},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {
					GPUs: 4,
				},
			},
			Queues: []test_utils.TestQueueBasic{
				{
					Name:         "queue1",
					DeservedGPUs: 2,
				},
			},
			Mocks: &test_utils.TestMock{
				CacheRequirements: &test_utils.CacheMocking{
					NumberOfCacheBinds: 2,
				},
			},
		},
		controller,
	)
	ssn.Cache = &failingBindCache{Cache: ssn.Cache}

	allocateAction := allocate.New()
	allocateAction.Execute(ssn)
}

type failingBindCache struct {
	cache.Cache
}

func (f *failingBindCache) Bind(podInfo *pod_info.PodInfo, hostname string, bindRequestAnnotations map[string]string) error {
	return fmt.Errorf("create pod error")
}
