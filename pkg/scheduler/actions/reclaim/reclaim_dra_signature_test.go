// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	commonconstants "github.com/kai-scheduler/api/constants"
)

// A job whose claim cannot be satisfied by any reclaim must not cause a later
// job in the same queue, with a different claim, to be skipped. nic-claim asks
// for more devices than the cluster has, and its class is not a GPU class, so
// ResReqVector does not reflect it and gpu-job looks no easier than nic-job.
func TestReclaimWithSchedulingSignaturesDoesNotSkipJobAfterFailedDRAJob(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	topology := test_utils.TestTopologyBasic{
		Name: "failed job with an unsatisfiable claim does not block a job with another claim",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:      "nic-job",
				Namespace: "test",
				Priority:  constants.PriorityTrainNumber,
				QueueName: "reclaiming-queue",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:              pod_status.Pending,
						ResourceClaimNames: []string{"nic-claim"},
					},
				},
			},
			{
				Name:      "gpu-job",
				Namespace: "test",
				Priority:  constants.PriorityTrainNumber,
				QueueName: "reclaiming-queue",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:              pod_status.Pending,
						ResourceClaimNames: []string{"gpu-claim"},
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
			DeviceClasses: []string{"nvidia.com/gpu", "rdma.example.com"},
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
					Name:            "nic-claim",
					Namespace:       "test",
					DeviceClassName: "rdma.example.com",
					Count:           3,
					Labels:          map[string]string{commonconstants.DefaultQueueLabel: "reclaiming-queue"},
				},
				{
					Name:            "gpu-claim",
					Namespace:       "test",
					DeviceClassName: "nvidia.com/gpu",
					Count:           2,
					Labels:          map[string]string{commonconstants.DefaultQueueLabel: "reclaiming-queue"},
				},
				{
					Name:            "victim-claim",
					Namespace:       "test",
					DeviceClassName: "nvidia.com/gpu",
					Count:           2,
					Labels:          map[string]string{commonconstants.DefaultQueueLabel: "victim-queue"},
					ClaimStatus:     allocatedClaimStatus("victim-claim", "victim-job-0", "0", "1"),
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
			"nic-job":    {Status: pod_status.Pending},
			"gpu-job":    {Status: pod_status.Pipelined, NodeName: "node0"},
			"victim-job": {Status: pod_status.Releasing, NodeName: "node0"},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  1,
				NumberOfPipelineActions: 1,
			},
		},
	}

	ssn := test_utils.BuildSession(topology, controller)
	ssn.SchedulerParams.UseSchedulingSignatures = true
	reclaim.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, topology, ssn)
}

func allocatedClaimStatus(request, podName string, devices ...string) *resourceapi.ResourceClaimStatus {
	results := make([]resourceapi.DeviceRequestAllocationResult, 0, len(devices))
	for _, device := range devices {
		results = append(results, resourceapi.DeviceRequestAllocationResult{
			Request: request,
			Driver:  "nvidia.com/gpu",
			Pool:    "node0",
			Device:  device,
		})
	}
	return &resourceapi.ResourceClaimStatus{
		Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{Results: results},
		},
		ReservedFor: []resourceapi.ResourceClaimConsumerReference{
			{Resource: "pods", Name: podName, UID: types.UID(podName)},
		},
	}
}
