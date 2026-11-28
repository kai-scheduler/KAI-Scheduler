// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A device of an earlier victim that is still terminating is freed without another eviction.
// The solver must count it as free, as the next session's allocate will: evict only the
// running pod that holds the other device, and pipeline the gang onto both.
func TestReclaimDRAWithDevicesOfReleasingVictims(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	task := func(state pod_status.PodStatus, nodeName, claimName string) *tasks_fake.TestTaskBasic {
		return &tasks_fake.TestTaskBasic{State: state, NodeName: nodeName, ResourceClaimNames: []string{claimName}}
	}

	testMetadata := test_utils.TestTopologyBasic{
		Name: "Gang needs a releasing victim's device and a running pod's device: evict only the running pod",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name: "releasing_job0", Namespace: "test", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
				Tasks: []*tasks_fake.TestTaskBasic{task(pod_status.Releasing, "node0", "releasing-claim")},
			},
			{
				Name: "running_job0", Namespace: "test", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
				Tasks: []*tasks_fake.TestTaskBasic{task(pod_status.Running, "node0", "running-claim")},
			},
			{
				Name: "pending_job0", Namespace: "test", QueueName: "queue1", Priority: constants.PriorityTrainNumber,
				Tasks: []*tasks_fake.TestTaskBasic{
					task(pod_status.Pending, "", "pending-claim-0"),
					task(pod_status.Pending, "", "pending-claim-1"),
				},
			},
		},
		TestDRAObjects: dra_fake.TestDRAObjects{
			DeviceClasses: []string{"nvidia.com/gpu"},
			ResourceSlices: []*dra_fake.TestResourceSlice{
				{Name: "node0-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: "node0", Count: 2},
			},
			ResourceClaims: []*dra_fake.TestResourceClaim{
				dra_fake.NewTestResourceClaim("releasing-claim", "test", "queue0").WithAllocation("node0", "0", "releasing_job0-0").GeneratedFor("releasing_job0-0"),
				dra_fake.NewTestResourceClaim("running-claim", "test", "queue0").WithAllocation("node0", "1", "running_job0-0"),
				dra_fake.NewTestResourceClaim("pending-claim-0", "test", "queue1"),
				dra_fake.NewTestResourceClaim("pending-claim-1", "test", "queue1"),
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}},
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
			{Name: "queue1", DeservedGPUs: 2, GPUOverQuotaWeight: 2},
		},
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheEvictions:  1,
				NumberOfPipelineActions: 2,
			},
		},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"releasing_job0": {Status: pod_status.Releasing, NodeName: "node0"},
			"running_job0":   {Status: pod_status.Releasing, NodeName: "node0"},
			"pending_job0":   {Status: pod_status.Pipelined, NodeName: "node0"},
		},
	}

	ssn := test_utils.BuildSession(testMetadata, controller)
	// The DRA manager cache is populated asynchronously.
	time.Sleep(time.Millisecond)
	reclaim.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, testMetadata, ssn)
}
