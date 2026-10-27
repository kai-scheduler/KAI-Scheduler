// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	"github.com/kai-scheduler/api/constants"
)

type deviceHolder struct {
	jobName string
	state   pod_status.PodStatus
}

func TestDRAFilter_AllocateVsPipeline(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	for _, test := range []struct {
		name               string
		holders            []deviceHolder
		expectAllocatable  bool
		expectPipelineable bool
	}{
		{
			name:               "device free",
			expectAllocatable:  true,
			expectPipelineable: true,
		},
		{
			name:               "device held by a running pod",
			holders:            []deviceHolder{{jobName: "holder-0", state: pod_status.Running}},
			expectAllocatable:  false,
			expectPipelineable: false,
		},
		{
			name:               "device held only by a terminating pod",
			holders:            []deviceHolder{{jobName: "holder-0", state: pod_status.Releasing}},
			expectAllocatable:  false,
			expectPipelineable: true,
		},
		{
			name:               "device held only by terminating pods",
			holders:            []deviceHolder{{jobName: "holder-0", state: pod_status.Releasing}, {jobName: "holder-1", state: pod_status.Releasing}},
			expectAllocatable:  false,
			expectPipelineable: true,
		},
		{
			name:               "shared claim held by a terminating and a running pod",
			holders:            []deviceHolder{{jobName: "holder-0", state: pod_status.Releasing}, {jobName: "holder-1", state: pod_status.Running}},
			expectAllocatable:  false,
			expectPipelineable: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ssn := setupTestSession(t, singleDeviceTopology(test.holders, []string{"pending-0"}), controller)
			task, job := pendingTask(t, ssn, "pending-0")
			node := ssn.ClusterInfo.Nodes["node0"]

			assert.Equal(t, test.expectAllocatable, ssn.PredicateFn(task, job, node, false) == nil, "allocate")
			assert.Equal(t, test.expectPipelineable, ssn.PredicateFn(task, job, node, true) == nil, "pipeline")
			// The pipeline check must not leak the released device into later allocation checks.
			assert.Equal(t, test.expectAllocatable, ssn.PredicateFn(task, job, node, false) == nil, "allocate after pipeline")
		})
	}
}

func TestDRAPipeline_ReleasingDeviceIsReservedByPipelinedPod(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	holders := []deviceHolder{{jobName: "holder-0", state: pod_status.Releasing}}
	ssn := setupTestSession(t, singleDeviceTopology(holders, []string{"pending-0", "pending-1"}), controller)
	first, firstJob := pendingTask(t, ssn, "pending-0")
	second, secondJob := pendingTask(t, ssn, "pending-1")
	node := ssn.ClusterInfo.Nodes["node0"]

	require.NoError(t, ssn.PredicateFn(first, firstJob, node, true))
	stmt := ssn.Statement()
	cp := stmt.Checkpoint()
	require.NoError(t, stmt.Pipeline(first, "node0", false))

	claimInfo := first.ResourceClaimInfo["claim-pending-0"]
	require.NotNil(t, claimInfo)
	require.NotNil(t, claimInfo.Allocation, "pipelined pod should be allocated the releasing device")
	require.Len(t, claimInfo.Allocation.Devices.Results, 1)
	assert.Equal(t, "0", claimInfo.Allocation.Devices.Results[0].Device)

	assert.Error(t, ssn.PredicateFn(second, secondJob, node, true),
		"device already promised to the pipelined pod must not be pipelined again")

	require.NoError(t, stmt.Rollback(cp))
	assert.NoError(t, ssn.PredicateFn(second, secondJob, node, true),
		"device is free for pipelining again once the first pod is unpipelined")
	assert.Error(t, ssn.PredicateFn(second, secondJob, node, false))
}

// singleDeviceTopology builds node0 with one DRA device, held by a single claim reserved for all holders,
// and pending jobs that each request one device through their own claim.
func singleDeviceTopology(holders []deviceHolder, pendingJobs []string) test_utils.TestTopologyBasic {
	queueLabel := map[string]string{constants.DefaultQueueLabel: "q-1"}
	topology := test_utils.TestTopologyBasic{
		Queues: []test_utils.TestQueueBasic{{Name: "q-1"}},
		Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {}},
		TestDRAObjects: dra_fake.TestDRAObjects{
			DeviceClasses: []string{"nvidia.com/gpu"},
			ResourceSlices: []*dra_fake.TestResourceSlice{
				{Name: "node0-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: "node0", Count: 1},
			},
		},
	}

	if len(holders) > 0 {
		holderClaim := &dra_fake.TestResourceClaim{
			Name:            "held-claim",
			Namespace:       "test",
			DeviceClassName: "nvidia.com/gpu",
			Count:           1,
			Labels:          queueLabel,
			ClaimStatus: &resourceapi.ResourceClaimStatus{
				Allocation: &resourceapi.AllocationResult{
					Devices: resourceapi.DeviceAllocationResult{
						Results: []resourceapi.DeviceRequestAllocationResult{
							{Request: "request", Driver: "nvidia.com/gpu", Pool: "node0", Device: "0"},
						},
					},
				},
			},
		}
		for _, holder := range holders {
			podName := fmt.Sprintf("%s-0", holder.jobName)
			holderClaim.ClaimStatus.ReservedFor = append(holderClaim.ClaimStatus.ReservedFor,
				resourceapi.ResourceClaimConsumerReference{Resource: "pods", Name: podName, UID: types.UID(podName)})
			topology.Jobs = append(topology.Jobs, &jobs_fake.TestJobBasic{
				Name:      holder.jobName,
				Namespace: "test",
				QueueName: "q-1",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State: holder.state,
						DeletionTimestamp: func() *metav1.Time {
							if holder.state != pod_status.Releasing {
								return nil
							}
							now := metav1.Now()
							return &now
						}(),
						NodeName:           "node0",
						ResourceClaimNames: []string{"held-claim"},
					},
				},
			})
		}
		topology.TestDRAObjects.ResourceClaims = append(topology.TestDRAObjects.ResourceClaims, holderClaim)
	}

	for _, jobName := range pendingJobs {
		claimName := "claim-" + jobName
		topology.TestDRAObjects.ResourceClaims = append(topology.TestDRAObjects.ResourceClaims, &dra_fake.TestResourceClaim{
			Name:            claimName,
			Namespace:       "test",
			DeviceClassName: "nvidia.com/gpu",
			Count:           1,
			Labels:          queueLabel,
		})
		topology.Jobs = append(topology.Jobs, &jobs_fake.TestJobBasic{
			Name:      jobName,
			Namespace: "test",
			QueueName: "q-1",
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Pending, ResourceClaimNames: []string{claimName}},
			},
		})
	}
	return topology
}

func pendingTask(t *testing.T, ssn *framework.Session, jobName string) (*pod_info.PodInfo, *podgroup_info.PodGroupInfo) {
	task := getTaskByStatus(t, ssn, jobName, pod_status.Pending)
	job := ssn.ClusterInfo.PodGroupInfos[common_info.NewPodGroupID("test", jobName)]
	require.NoError(t, ssn.PrePredicateFn(task, job))
	return task, job
}
