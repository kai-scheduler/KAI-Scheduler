// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate

import (
	"testing"
	"time"

	. "go.uber.org/mock/gomock"

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

// On a node whose GPUs come only from DRA, a pod requesting nvidia.com/gpu needs a DRA claim to get one.
func TestGpuRequestOnDRAOnlyNode(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	integration_tests_utils.SetSchedulerActions()
	controller := NewController(t)
	defer controller.Finish()

	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	draOnlyNode := func() (map[string]nodes_fake.TestNodeBasic, dra_fake.TestDRAObjects) {
		return map[string]nodes_fake.TestNodeBasic{"node0": {}}, dra_fake.TestDRAObjects{
			DeviceClasses: []string{"nvidia.com/gpu"},
			ResourceSlices: []*dra_fake.TestResourceSlice{
				{Name: "node0-gpu", DeviceClassName: "nvidia.com/gpu", NodeName: "node0", Count: 1},
			},
			ResourceClaims: []*dra_fake.TestResourceClaim{
				dra_fake.NewTestResourceClaim("claim-0", "test", "queue1"),
			},
		}
	}

	nodes, draObjects := draOnlyNode()
	podRequestingGpuResource := integration_tests_utils.TestTopologyMetadata{
		Name: "pod requesting nvidia.com/gpu without a claim is not scheduled on a DRA-only node",
		TestTopologyBasic: test_utils.TestTopologyBasic{
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "pending_job0",
					Namespace:           "test",
					Priority:            constants.PriorityTrainNumber,
					QueueName:           "queue1",
					RequiredGPUsPerTask: 1,
					Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes:          nodes,
			TestDRAObjects: draObjects,
			Queues:         []test_utils.TestQueueBasic{{Name: "queue1", DeservedGPUs: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {GPUsRequired: 1, Status: pod_status.Pending},
			},
		},
		RoundsUntilMatch:   2,
		RoundsAfterMatch:   1,
		SchedulingDuration: 1 * time.Millisecond,
	}

	nodes, draObjects = draOnlyNode()
	podWithClaim := integration_tests_utils.TestTopologyMetadata{
		Name: "pod with a GPU claim is scheduled on a DRA-only node",
		TestTopologyBasic: test_utils.TestTopologyBasic{
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:      "pending_job0",
					Namespace: "test",
					Priority:  constants.PriorityTrainNumber,
					QueueName: "queue1",
					Tasks: []*tasks_fake.TestTaskBasic{
						{State: pod_status.Pending, ResourceClaimNames: []string{"claim-0"}},
					},
				},
			},
			Nodes:          nodes,
			TestDRAObjects: draObjects,
			Queues:         []test_utils.TestQueueBasic{{Name: "queue1", DeservedGPUs: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {NodeName: "node0", Status: pod_status.Running},
			},
			Mocks: &test_utils.TestMock{
				CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 1},
			},
		},
		RoundsUntilMatch:   10, // The draManager cache races with the test, so it needs more rounds to match
		RoundsAfterMatch:   1,
		SchedulingDuration: 1 * time.Millisecond,
	}

	for testNumber, testData := range []integration_tests_utils.TestTopologyMetadata{podRequestingGpuResource, podWithClaim} {
		t.Run(testData.Name, func(t *testing.T) {
			testData.TestTopologyBasic.Name = testData.Name
			integration_tests_utils.RunTest(t, testData, testNumber, controller)
		})
	}
}
