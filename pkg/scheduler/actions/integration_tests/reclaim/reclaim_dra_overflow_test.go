// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"math"
	"testing"
	"time"

	"gopkg.in/h2non/gock.v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"

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

func TestDRAOverflowDoesNotBlockReclaimInSameQueue(t *testing.T) {
	defer gock.Off()

	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "overflowing DRA request does not block reclaim for a valid request in the same queue",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "overflowing DRA request does not block reclaim for a valid request in the same queue",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "valid-job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "reclaiming-queue",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"valid-claim"},
							},
						},
					},
					{
						Name:      "overflow-job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "reclaiming-queue",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
								ResourceClaimNames: []string{
									"overflow-claim-0",
									"overflow-claim-1",
								},
							},
						},
					},
					{
						Name:      "victim-job",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "victim-queue",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"victim-claim"},
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
							Count:           2,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "valid-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "reclaiming-queue",
							},
						},
						{
							Name:            "overflow-claim-0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           math.MaxInt64,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "reclaiming-queue",
							},
						},
						{
							Name:            "overflow-claim-1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           math.MaxInt64,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "reclaiming-queue",
							},
						},
						{
							Name:            "victim-claim",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "victim-queue",
							},
							ClaimStatus: allocatedDRAClaimStatus("victim-job-0"),
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "reclaiming-queue",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "victim-queue",
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"valid-job":    {Status: pod_status.Running, NodeName: "node0"},
					"overflow-job": {Status: pod_status.Pending},
					"victim-job":   {Status: pod_status.Pending},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: time.Millisecond,
		},
	})
}

func allocatedDRAClaimStatus(podName string) *resourceapi.ResourceClaimStatus {
	return &resourceapi.ResourceClaimStatus{
		Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{
				Results: []resourceapi.DeviceRequestAllocationResult{
					{
						Request: "victim-claim",
						Driver:  "nvidia.com/gpu",
						Pool:    "node0",
						Device:  "0",
					},
					{
						Request: "victim-claim",
						Driver:  "nvidia.com/gpu",
						Pool:    "node0",
						Device:  "1",
					},
				},
			},
		},
		ReservedFor: []resourceapi.ResourceClaimConsumerReference{
			{Resource: "pods", Name: podName, UID: types.UID(podName)},
		},
	}
}
