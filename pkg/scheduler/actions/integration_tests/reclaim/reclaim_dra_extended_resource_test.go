// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"
	"time"

	"gopkg.in/h2non/gock.v1"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	integration_tests_utils "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/dra_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// A victim holding its GPU through its extended resource claim (KEP-5004) must free it when reclaimed.
func TestReclaimDRAExtendedResourceVictimIntegrationTest(t *testing.T) {
	defer gock.Off()

	featuregates.SetDynamicResourcesEnabledForTest(true)
	t.Cleanup(func() {
		featuregates.SetDynamicResourcesEnabledForTest(false)
	})

	integration_tests_utils.RunTests(t, []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "victim holds the only GPU through its extended resource claim - reclaim",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "victim holds the only GPU through its extended resource claim - reclaim",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:      "running_job0",
						Namespace: "test",
						Priority:  constants.PriorityTrainNumber,
						QueueName: "queue0",
						// The pod requests nvidia.com/gpu, which is how its GPU is counted.
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:                  "node0",
								State:                     pod_status.Running,
								ExtendedResourceClaimName: "running-job0-extended-resources",
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
							Count:           1,
						},
					},
					ResourceClaims: []*dra_fake.TestResourceClaim{
						dra_fake.NewTestResourceClaim("running-job0-extended-resources", "test", "queue0").
							WithAllocation("node0", "0", "running_job0-0").
							ExtendedResourceFor("running_job0-0"),
						dra_fake.NewTestResourceClaim("claim-pending-job0", "test", "queue1"),
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       0,
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
						GPUsRequired: 1,
						Status:       pod_status.Pending,
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
	})
}
