// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	resourceapi "k8s.io/api/resource/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestDRAAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	integration_tests_utils.SetSchedulerActions()
	controller := NewController(t)
	defer controller.Finish()

	featuregate.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DynamicResourceAllocation, true)

	for testNumber, testData := range []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "Simple pod with no resource claim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status: pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "Simple pod with simple resource claim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   10, // This one is flaky and needs more rounds to match because of the draManager cache racing with the test
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "Simple pod with simple resource claim - requesting too many devices",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "2 pods requesting node-bound device, can't schedule on same node",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "pending_job0",
						Namespace:           "test",
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue1",
						RequiredCPUsPerTask: 1000,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						CPUMillis: 1000,
					},
					"node1": {
						CPUMillis: 1000,
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
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "2 simple pods with simple resource claims, allocating on separate nodes",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
					{
						Name:      "pending_job1",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-1"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
						{
							Name:            "claim-1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
					"node1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
					"pending_job1": {
						Status:   pod_status.Running,
						NodeName: "node1",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch:   10,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "Exactly at claim max consumers limit",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							ReservedFor:     dra_fake.RandomReservedForReferences(resourceapi.ResourceClaimReservedForMaxSize - 2),
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status:   pod_status.Running,
						NodeName: "node0",
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 2,
					},
				},
			},
			RoundsUntilMatch:   10,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "Partially over claim max consumers limit",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							ReservedFor:     dra_fake.RandomReservedForReferences(resourceapi.ResourceClaimReservedForMaxSize - 2),
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "Claim already reached max consumers limit",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-0"},
							},
						},
					},
				},
				TestDRAObjects: dra_fake.TestDRAObjects{
					DeviceClasses: []string{"nvidia.com/gpu"},
					ResourceSlices: []*dra_fake.TestResourceSlice{
						{
							Name:            "node0-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node0",
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							ReservedFor:     dra_fake.RandomReservedForReferences(resourceapi.ResourceClaimReservedForMaxSize),
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue1",
						DeservedGPUs: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"pending_job0": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
	} {
		t.Run(testData.Name, func(t *testing.T) {
			testData.TestTopologyBasic.Name = testData.Name
			integration_tests_utils.RunTest(t, testData, testNumber, controller)
		})
	}
}
