// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
	commonconstants "github.com/kai-scheduler/api/constants"
)

type draDeviceHolder struct {
	jobName           string
	state             pod_status.PodStatus
	deletionTimestamp *metav1.Time
}

func TestAllocateDRAPipelineOntoTerminatingPods(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	now := metav1.Now()
	for testNumber, topology := range []test_utils.TestTopologyBasic{
		draHeldDeviceTopology(
			"Device is free - pending pod is allocated",
			nil, 1,
			map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {Status: pod_status.Binding, NodeName: "node0"},
			},
			&test_utils.CacheMocking{NumberOfCacheBinds: 1},
		),
		draHeldDeviceTopology(
			"Only device is held by a terminating pod - pending pod is pipelined",
			[]draDeviceHolder{{jobName: "holder_job0", state: pod_status.Releasing, deletionTimestamp: &now}}, 1,
			map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pipelined, NodeName: "node0"},
			},
			&test_utils.CacheMocking{NumberOfPipelineActions: 1},
		),
		draHeldDeviceTopology(
			"One device held by a terminating pod, one free - pending pod is allocated on the free one",
			[]draDeviceHolder{{jobName: "holder_job0", state: pod_status.Releasing, deletionTimestamp: &now}}, 2,
			map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Binding, NodeName: "node0"},
			},
			&test_utils.CacheMocking{NumberOfCacheBinds: 1},
		),
		draHeldDeviceTopology(
			"Only device is held by a running pod - pending pod stays pending",
			[]draDeviceHolder{{jobName: "holder_job0", state: pod_status.Running}}, 1,
			map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Running, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pending},
			},
			&test_utils.CacheMocking{},
		),
		draHeldDeviceTopology(
			"Only device is shared by a terminating and a running pod - pending pod stays pending",
			[]draDeviceHolder{
				{jobName: "holder_job0", state: pod_status.Releasing, deletionTimestamp: &now},
				{jobName: "holder_job1", state: pod_status.Running},
			}, 1,
			map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"holder_job1":  {Status: pod_status.Running, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pending},
			},
			&test_utils.CacheMocking{},
		),
	} {
		t.Run(topology.Name, func(t *testing.T) {
			ssn := test_utils.BuildSession(topology, controller)
			allocate.New().Execute(ssn)
			test_utils.MatchExpectedAndRealTasks(t, testNumber, topology, ssn)
		})
	}
}

// draHeldDeviceTopology builds node0 with deviceCount DRA devices. Device "0" is allocated to one claim
// shared by all holders, and a pending job requests one device.
func draHeldDeviceTopology(
	name string, holders []draDeviceHolder, deviceCount int,
	expected map[string]test_utils.TestExpectedResultBasic, cacheMocking *test_utils.CacheMocking,
) test_utils.TestTopologyBasic {
	queueLabel := map[string]string{commonconstants.DefaultQueueLabel: "queue0"}
	claims := []*dra_fake.TestResourceClaim{
		{Name: "claim-0", Namespace: "test", DeviceClassName: "nvidia.com/gpu", Count: 1, Labels: queueLabel},
	}
	jobs := []*jobs_fake.TestJobBasic{
		{
			Name:      "pending_job0",
			Namespace: "test",
			Priority:  constants.PriorityTrainNumber,
			QueueName: "queue0",
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Pending, ResourceClaimNames: []string{"claim-0"}},
			},
		},
	}

	if len(holders) > 0 {
		heldClaim := &dra_fake.TestResourceClaim{
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
			podName := holder.jobName + "-0"
			heldClaim.ClaimStatus.ReservedFor = append(heldClaim.ClaimStatus.ReservedFor,
				resourceapi.ResourceClaimConsumerReference{Resource: "pods", Name: podName, UID: types.UID(podName)})
			jobs = append(jobs, &jobs_fake.TestJobBasic{
				Name:      holder.jobName,
				Namespace: "test",
				Priority:  constants.PriorityTrainNumber,
				QueueName: "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:              holder.state,
						DeletionTimestamp:  holder.deletionTimestamp,
						NodeName:           "node0",
						ResourceClaimNames: []string{"held-claim"},
					},
				},
			})
		}
		claims = append(claims, heldClaim)
	}

	return test_utils.TestTopologyBasic{
		Name: name,
		Jobs: jobs,
		TestDRAObjects: dra_fake.TestDRAObjects{
			DeviceClasses: []string{"nvidia.com/gpu"},
			ResourceSlices: []*dra_fake.TestResourceSlice{
				{Name: "node0-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: "node0", Count: deviceCount},
			},
			ResourceClaims: claims,
		},
		Nodes:              map[string]nodes_fake.TestNodeBasic{"node0": {}},
		Queues:             []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2}},
		JobExpectedResults: expected,
		Mocks:              &test_utils.TestMock{CacheRequirements: cacheMocking},
	}
}
