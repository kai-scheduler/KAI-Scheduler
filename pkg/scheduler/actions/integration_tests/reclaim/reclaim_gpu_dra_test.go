// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"
	"time"

	"gopkg.in/h2non/gock.v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregate "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	integration_tests_utils "github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
	resourceapi "k8s.io/api/resource/v1"
)

func TestReclaimGpuDRAIntegrationTest(t *testing.T) {
	defer gock.Off()

	featuregate.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DynamicResourceAllocation, true)

	// NOTE: These tests currently fail because reclaim with DRA resources may not be fully implemented yet.
	// The tests are correctly structured and should pass once DRA reclaim support is complete.
	// Current behavior: victim pods are not being evicted/reclaimed when using DRA resource claims.
	integration_tests_utils.RunTests(t, getReclaimGpuDRATestsMetadata())
}

func getReclaimGpuDRATestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "queue0 is under fair share, queue1 is over fair share - reclaim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "queue0 is under fair share, queue1 is over fair share - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "q0_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"claim-q0-job0"},
							},
						},
					},
					{
						Name:      "q0_job1",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"claim-q0-job1"},
							},
						},
					},
					{
						Name:      "q0_job2",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-q0-job2"},
							},
						},
					},
					{
						Name:      "q1_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"claim-q1-job0"},
							},
						},
					},
					{
						Name:      "q1_job1",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"claim-q1-job1"},
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
							Count:           8,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-q0-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-q0-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "0",
											},
											{
												Request: "claim-q0-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "1",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "q0_job0-0", UID: "q0_job0-0"},
								},
							},
						},
						{
							Name:            "claim-q0-job1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-q0-job1",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "2",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "q0_job1-0", UID: "q0_job1-0"},
								},
							},
						},
						{
							Name:            "claim-q0-job2",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
						},
						{
							Name:            "claim-q1-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           4,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue1",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-q1-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "3",
											},
											{
												Request: "claim-q1-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "4",
											},
											{
												Request: "claim-q1-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "5",
											},
											{
												Request: "claim-q1-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "6",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "q1_job0-0", UID: "q1_job0-0"},
								},
							},
						},
						{
							Name:            "claim-q1-job1",
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
												Request: "claim-q1-job1",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "7",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "q1_job1-0", UID: "q1_job1-0"},
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
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 6,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       4,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"q0_job0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"q0_job1": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"q0_job2": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"q1_job0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"q1_job1": {
						Status: pod_status.Pending,
					},
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
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "1 train job running for queue0, 1 train job pending for queue1 - reclaim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "1 train job running for queue0, 1 train job pending for queue1 - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:           "node0",
								State:              pod_status.Running,
								ResourceClaimNames: []string{"claim-running-job0"},
							},
						},
					},
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-pending-job0"},
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
							Name:            "claim-running-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-running-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "0",
											},
											{
												Request: "claim-running-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "1",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "running_job0-0", UID: "running_job0-0"},
								},
							},
						},
						{
							Name:            "claim-pending-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue1",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						Status: pod_status.Pending,
					},
					"pending_job0": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
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
			SchedulingDuration: 1 * time.Millisecond,
		},
		{
			Name: "2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1, deserved of queue1 is 1 -  don't reclaim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "2 jobs running for queue0 on 3 GPUs, 1 job running for queue1 on 1 GPU, 1 pending job for queue1, deserved of queue1 is 1 -  don't reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Running,
								NodeName:           "node1",
								ResourceClaimNames: []string{"claim-running-job0"},
							},
						},
					},
					{
						Name:      "running_job1",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Running,
								NodeName:           "node0",
								ResourceClaimNames: []string{"claim-running-job1"},
							},
						},
					},
					{
						Name:      "running_job2",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Running,
								NodeName:           "node1",
								ResourceClaimNames: []string{"claim-running-job2"},
							},
						},
					},
					{
						Name:      "pending_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:              pod_status.Pending,
								ResourceClaimNames: []string{"claim-pending-job0"},
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
						{
							Name:            "node1-gpu",
							DeviceClassName: "nvidia.com/gpu",
							NodeName:        "node1",
							Count:           2,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						{
							Name:            "claim-running-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-running-job0",
												Driver:  "nvidia.com/gpu",
												Pool:    "node1",
												Device:  "0",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "running_job0-0", UID: "running_job0-0"},
								},
							},
						},
						{
							Name:            "claim-running-job1",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           2,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue0",
							},
							ClaimStatus: &resourceapi.ResourceClaimStatus{
								Allocation: &resourceapi.AllocationResult{
									Devices: resourceapi.DeviceAllocationResult{
										Results: []resourceapi.DeviceRequestAllocationResult{
											{
												Request: "claim-running-job1",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "0",
											},
											{
												Request: "claim-running-job1",
												Driver:  "nvidia.com/gpu",
												Pool:    "node0",
												Device:  "1",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "running_job1-0", UID: "running_job1-0"},
								},
							},
						},
						{
							Name:            "claim-running-job2",
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
												Request: "claim-running-job2",
												Driver:  "nvidia.com/gpu",
												Pool:    "node1",
												Device:  "1",
											},
										},
									},
								},
								ReservedFor: []resourceapi.ResourceClaimConsumerReference{
									{Resource: "pods", Name: "running_job2-0", UID: "running_job2-0"},
								},
							},
						},
						{
							Name:            "claim-pending-job0",
							Namespace:       "test",
							DeviceClassName: "nvidia.com/gpu",
							Count:           1,
							Labels: map[string]string{
								commonconstants.DefaultQueueLabel: "queue1",
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
					"node1": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
					{
						Name:               "queue1",
						DeservedGPUs:       1,
						GPUOverQuotaWeight: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName: "node1",
						Status:   pod_status.Running,
					},
					"running_job1": {
						NodeName: "node0",
						Status:   pod_status.Running,
					},
					"running_job2": {
						NodeName: "node1",
						Status:   pod_status.Running,
					},
					"pending_job0": {
						Status: pod_status.Pending,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:     0,
						NumberOfCacheEvictions: 0,
					},
				},
			},
			RoundsUntilMatch:   2,
			RoundsAfterMatch:   1,
			SchedulingDuration: 1 * time.Millisecond,
		},
	}
}
