// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate

import (
	"testing"
	"time"

	resourceapi "k8s.io/api/resource/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestSharedDRADeviceDoesNotBlockCPUOnlyPod(t *testing.T) {
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "shared DRA device does not block CPU-only pod",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "shared DRA device does not block CPU-only pod",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "shared_dra_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"shared-claim"},
							},
						},
					},
					{
						Name:      "shared_dra_job1",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"shared-claim"},
							},
						},
					},
					{
						Name:      "cpu_only_job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Pending,
								NodeAffinityNames: []string{"node0"},
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
							Name:            "shared-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "request",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "shared_dra_job0-0", UID: "shared_dra_job0-0"},
									{Resource: "pods", Name: "shared_dra_job1-0", UID: "shared_dra_job1-0"},
								},
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
					"shared_dra_job0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"shared_dra_job1": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"cpu_only_job": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   1,
			RoundsAfterMatch:   1,
			SchedulingDuration: time.Millisecond,
		},
	})
}

func TestPendingPodCanUseSharedDRADevice(t *testing.T) {
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "pending pod can use shared DRA device",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "pending pod can use shared DRA device",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_shared_dra_job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"shared-claim"},
							},
						},
					},
					{
						Name:      "pending_shared_dra_job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								NodeAffinityNames:  []string{"node0"},
								ResourceClaimNames: []string{"shared-claim"},
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
							Name:            "shared-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "request",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "running_shared_dra_job-0", UID: "running_shared_dra_job-0"},
									{Resource: "pods", Name: "pending_shared_dra_job-0", UID: "pending_shared_dra_job-0"},
								},
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
					"running_shared_dra_job": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"pending_shared_dra_job": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
			RoundsUntilMatch:   1,
			RoundsAfterMatch:   1,
			SchedulingDuration: time.Millisecond,
		},
	})
}
