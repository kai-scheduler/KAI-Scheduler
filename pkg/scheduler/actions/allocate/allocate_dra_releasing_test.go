// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"
	resourceapi "k8s.io/api/resource/v1"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A device held only by a terminating pod's claim is freed once the pod exits, like the pod's
// other resources, so allocate pipelines onto it instead of leaving the job pending.
func TestAllocateDRAWithDevicesOfReleasingPods(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	for testNumber, testMetadata := range []test_utils.TestTopologyBasic{
		{
			Name: "Only device held by a releasing pod: pipeline onto it",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim"),
			},
			TestDRAObjects: draObjects(1,
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0").GeneratedFor("holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pipelined, NodeName: "node0"},
			},
		},
		{
			// The claim controller only deallocates claims carrying its finalizer.
			Name: "Only device held by a releasing pod's standalone claim: stay pending",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim"),
			},
			TestDRAObjects: draObjects(1,
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pending},
			},
		},
		{
			Name: "Only device held by a releasing pod's standalone claim with the deallocation finalizer: pipeline onto it",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim"),
			},
			TestDRAObjects: draObjects(1,
				withFinalizer(dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0")),
				dra_fake.NewTestResourceClaim("pending-claim", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pipelined, NodeName: "node0"},
			},
		},
		{
			Name: "Gang of two, one free device and one held by a releasing pod: pipeline the gang",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim-0", "pending-claim-1"),
			},
			TestDRAObjects: draObjects(2,
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0").GeneratedFor("holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim-0", "test", "queue0"),
				dra_fake.NewTestResourceClaim("pending-claim-1", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 2}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pipelined, NodeName: "node0"},
			},
		},
		{
			Name: "Gang of two, one device held by a releasing pod: the device is not pipelined twice",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim-0", "pending-claim-1"),
			},
			TestDRAObjects: draObjects(1,
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0").GeneratedFor("holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim-0", "test", "queue0"),
				dra_fake.NewTestResourceClaim("pending-claim-1", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pending},
			},
		},
		{
			// Control: a running pod's device is not freed by waiting.
			Name: "Only device held by a running pod: stay pending",
			Jobs: []*jobs_fake.TestJobBasic{
				draJob("holder_job0", pod_status.Running, "node0", "holder-claim"),
				draJob("pending_job0", pod_status.Pending, "", "pending-claim"),
			},
			TestDRAObjects: draObjects(1,
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").WithAllocation("node0", "0", "holder_job0-0").GeneratedFor("holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim", "test", "queue0"),
			),
			Nodes:  draNodes(),
			Queues: draQueues(),
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {Status: pod_status.Running, NodeName: "node0"},
				"pending_job0": {Status: pod_status.Pending},
			},
		},
	} {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.Name)
		ssn := test_utils.BuildSession(testMetadata, controller)
		// The DRA manager cache is populated asynchronously.
		time.Sleep(time.Millisecond)
		allocate.New().Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata, ssn)
	}
}

// A device reachable from every node is freed for all of them when a pod terminating on one node exits.
func TestAllocateDRAWithSharedDeviceOfReleasingPodOnAnotherNode(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	pendingJob := draJob("pending_job0", pod_status.Pending, "", "pending-claim")
	pendingJob.Tasks[0].NodeAffinityNames = []string{"node1"}
	testMetadata := test_utils.TestTopologyBasic{
		Name: "Shared device held by a pod releasing on another node: pipeline onto it",
		Jobs: []*jobs_fake.TestJobBasic{
			draJob("holder_job0", pod_status.Releasing, "node0", "holder-claim"),
			pendingJob,
		},
		TestDRAObjects: dra_fake.TestDRAObjects{
			DeviceClasses: []string{"nvidia.com/gpu"},
			ResourceSlices: []*dra_fake.TestResourceSlice{
				{Name: "shared-gpu", DeviceClassName: "nvidia.com/gpu", AllNodes: true, Count: 1},
			},
			ResourceClaims: []*dra_fake.TestResourceClaim{
				dra_fake.NewTestResourceClaim("holder-claim", "test", "queue0").
					WithAllocation("shared-gpu", "0", "holder_job0-0").GeneratedFor("holder_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim", "test", "queue0"),
			},
		},
		Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}, "node1": {GPUs: 2}},
		Queues: draQueues(),
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1}},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"holder_job0":  {Status: pod_status.Releasing, NodeName: "node0"},
			"pending_job0": {Status: pod_status.Pipelined, NodeName: "node1"},
		},
	}

	ssn := test_utils.BuildSession(testMetadata, controller)
	// The DRA manager cache is populated asynchronously.
	time.Sleep(time.Millisecond)
	allocate.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, testMetadata, ssn)
}

func draJob(name string, state pod_status.PodStatus, nodeName string, claims ...string) *jobs_fake.TestJobBasic {
	job := &jobs_fake.TestJobBasic{
		Name:      name,
		Namespace: "test",
		Priority:  constants.PriorityTrainNumber,
		QueueName: "queue0",
	}
	for _, claim := range claims {
		job.Tasks = append(job.Tasks, &tasks_fake.TestTaskBasic{
			State:              state,
			NodeName:           nodeName,
			ResourceClaimNames: []string{claim},
		})
	}
	return job
}

func draObjects(devices int, claims ...*dra_fake.TestResourceClaim) dra_fake.TestDRAObjects {
	return dra_fake.TestDRAObjects{
		DeviceClasses: []string{"nvidia.com/gpu"},
		ResourceSlices: []*dra_fake.TestResourceSlice{
			{Name: "node0-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: "node0", Count: devices},
		},
		ResourceClaims: claims,
	}
}

func withFinalizer(claim *dra_fake.TestResourceClaim) *dra_fake.TestResourceClaim {
	claim.Finalizers = []string{resourceapi.Finalizer}
	return claim
}

func draNodes() map[string]nodes_fake.TestNodeBasic {
	return map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}}
}

func draQueues() []test_utils.TestQueueBasic {
	return []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2}}
}
