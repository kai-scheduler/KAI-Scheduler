// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resizeeviction_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/resizeeviction"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleResizeEviction(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getTestsMetadata() {
		t.Logf("Running Test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		resizeEvictionAction := resizeeviction.New()
		resizeEvictionAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func cpu(value string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(value)}
}

func getTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "deferred resize evicts a lower priority job in the same queue",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {Status: pod_status.Releasing},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "same priority job in the same queue is not evicted",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "non-preemptible lower priority job is not evicted",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						Preemptibility:      enginev2alpha2.NonPreemptible,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "nothing is evicted when eligible victims cannot cover the shortfall",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "protected_job0",
						RequiredCPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3.5,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0":    {NodeName: "node0", Status: pod_status.Running},
					"protected_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job0-0":    {NodeName: "node0", Status: pod_status.Running},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no eviction when the deferred resize is already enactable",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "deferred resize reclaims from another queue that is over its deserved share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(4000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
					{
						Name:           "queue1",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(0),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {Status: pod_status.Releasing},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "no reclaim when the resizing queue is over its fair share",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1500m")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(0),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
					{
						Name:           "queue1",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(4000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "gang victim is evicted whole",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 3,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("2")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {Status: pod_status.Releasing},
					"victim_job0-1": {Status: pod_status.Releasing},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "unallocated targets of other deferred resizes are excluded from the shortfall",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "victim_job0",
						RequiredCPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running},
						},
					},
					{
						Name:                "resize_job0",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber + 20,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1")},
						},
					},
					{
						Name:                "resize_job1",
						RequiredCPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber + 10,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{NodeName: "node0", State: pod_status.Running, DeferredResizeActual: cpu("1")},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {CPUMillis: 4, CPUMemory: 100e9},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:           "queue0",
						DeservedCPUs:   test_utils.CreateFloat64Pointer(8000),
						DeservedMemory: test_utils.CreateFloat64Pointer(100e9),
					},
				},
				// resize_job0 is enactable once resize_job1's unallocated target is excluded;
				// resize_job1 then still needs the victim evicted.
				TaskExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"victim_job0-0": {Status: pod_status.Releasing},
					"resize_job0-0": {NodeName: "node0", Status: pod_status.Running},
					"resize_job1-0": {NodeName: "node0", Status: pod_status.Running},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions: 1,
					},
				},
			},
		},
	}
}
