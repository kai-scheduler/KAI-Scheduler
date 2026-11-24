// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestPreemptForDeferredResizeIntegrationTest(t *testing.T) {
	integration_tests_utils.RunTests(t, getDeferredResizePreemptTestsMetadata())
}

func getDeferredResizePreemptTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "deferred resize evicts once while the victim terminates and the kubelet has not enacted",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "resizer",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityInteractivePreemptibleNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{{
							NodeName:     "node0",
							State:        pod_status.Running,
							ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: 1, Deferred: true},
						}},
					},
					{
						Name:                "victim",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber - 10,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node0", State: pod_status.Running}},
					},
					{
						Name:                "bystander",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						DeleteJobInTest:     true,
						Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: "node0", State: pod_status.Running}},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 6},
				},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0"}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer":   {NodeName: "node0", Status: pod_status.Running},
					"victim":    {NodeName: "node0", Status: pod_status.Releasing},
					"bystander": {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1},
				},
				DeferredResizePreemption: true,
			},
			RoundsUntilMatch: 2,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "evicted victim re-queued as pending cannot take the reserved room back",
				Jobs: []*jobs_fake.TestJobBasic{
					deferredResizer(3, 1),
					runningCPU("victim", "queue0", constants.PriorityTrainNumber, 2, "node0"),
					{
						Name:                "intruder",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
					},
				},
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0"}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer":  {NodeName: "node0", Status: pod_status.Running},
					"victim":   {Status: pod_status.Pending},
					"intruder": {Status: pod_status.Pending},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1},
				},
				DeferredResizePreemption: true,
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 5,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "evicted victim re-queued as pending moves to a node with room",
				Jobs: []*jobs_fake.TestJobBasic{
					deferredResizer(3, 1),
					runningCPU("victim", "queue0", constants.PriorityTrainNumber, 2, "node0"),
				},
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}, "node1": {CPUMillis: 4}},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0"}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer": {NodeName: "node0", Status: pod_status.Running},
					"victim":  {NodeName: "node1", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions: 1, NumberOfCacheBinds: 10, NumberOfPipelineActions: 10,
					},
				},
				DeferredResizePreemption: true,
			},
			RoundsUntilMatch: 3,
			RoundsAfterMatch: 5,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim frees the room first and preempt then evicts nothing more",
				Jobs: []*jobs_fake.TestJobBasic{
					deferredResizer(3, 1),
					runningCPU("same-queue", "queue0", constants.PriorityTrainNumber, 1, "node0"),
					func() *jobs_fake.TestJobBasic {
						job := runningCPU("other-queue", "queue1", constants.PriorityTrainNumber, 2, "node0")
						job.DeleteJobInTest = true
						return job
					}(),
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}},
				Queues: []test_utils.TestQueueBasic{
					{
						Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(4000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100000),
					},
					{
						Name: "queue1", DeservedCPUs: test_utils.CreateFloat64Pointer(0),
						DeservedMemory: test_utils.CreateFloat64Pointer(100000),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer":     {NodeName: "node0", Status: pod_status.Running},
					"same-queue":  {NodeName: "node0", Status: pod_status.Running},
					"other-queue": {NodeName: "node0", Status: pod_status.Releasing},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1},
				},
				DeferredResizePreemption: true,
			},
			RoundsUntilMatch: 2,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaimed victim re-queued as pending does not reclaim back",
				Jobs: []*jobs_fake.TestJobBasic{
					deferredResizer(3, 1),
					runningCPU("other-queue", "queue1", constants.PriorityTrainNumber, 2, "node0"),
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}},
				Queues: []test_utils.TestQueueBasic{
					{
						Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(3000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100000),
					},
					{
						Name: "queue1", DeservedCPUs: test_utils.CreateFloat64Pointer(1000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100000),
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer":     {NodeName: "node0", Status: pod_status.Running},
					"other-queue": {Status: pod_status.Pending},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1},
				},
				DeferredResizePreemption: true,
			},
			RoundsUntilMatch: 2,
			RoundsAfterMatch: 5,
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "feature disabled keeps the cluster unchanged across rounds",
				Jobs: []*jobs_fake.TestJobBasic{
					deferredResizer(3, 1),
					runningCPU("victim", "queue0", constants.PriorityTrainNumber, 2, "node0"),
				},
				Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {CPUMillis: 4}},
				Queues: []test_utils.TestQueueBasic{{Name: "queue0"}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"resizer": {NodeName: "node0", Status: pod_status.Running},
					"victim":  {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			},
			RoundsUntilMatch: 2,
		},
	}
}

func deferredResizer(desiredCPUs, actualCPUs float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                "resizer",
		RequiredCPUsPerTask: desiredCPUs,
		Priority:            constants.PriorityInteractivePreemptibleNumber,
		QueueName:           "queue0",
		Tasks: []*tasks_fake.TestTaskBasic{{
			NodeName:     "node0",
			State:        pod_status.Running,
			ResizeStatus: &tasks_fake.TestResizeStatus{ActualCPUs: actualCPUs, Deferred: true},
		}},
	}
}

func runningCPU(name, queueName string, priority int32, cpus float64, nodeName string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredCPUsPerTask: cpus,
		Priority:            priority,
		QueueName:           queueName,
		Tasks:               []*tasks_fake.TestTaskBasic{{NodeName: nodeName, State: pod_status.Running}},
	}
}
