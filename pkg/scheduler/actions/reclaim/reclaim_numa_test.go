// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestHandleReclaimNuma drives the real reclaim action: an under-quota queue reclaims a NUMA zone
// held by an over-quota queue's victim. Both zones are occupied, so the reclaimer can only land
// after a victim is evicted to free a whole zone. Verifies the reclaimed victim, the reclaimer's
// placement on the freed zone, and a consistent per-zone ledger after evict+pipeline.
func TestHandleReclaimNuma(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	defer gock.Off()

	for testNumber, testMetadata := range getNumaReclaimTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.Name)

		ssn := test_utils.BuildSession(testMetadata, controller)
		reclaim.New().Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata, ssn)
	}
}

func cpu(amount string) map[v1.ResourceName]string {
	return map[v1.ResourceName]string{v1.ResourceCPU: amount}
}

func getNumaReclaimTestsMetadata() []test_utils.TestTopologyBasic {
	return []test_utils.TestTopologyBasic{
		{
			Name: "Under-quota queue reclaims a NUMA zone from an over-quota queue's victim",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					// Lower priority within the over-quota queue, so this victim is reclaimed.
					Name: "victim0", QueueName: "victim_queue", Priority: constants.PriorityTrainNumber - 1,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-0": cpu("3")}),
					}},
				},
				{
					Name: "victim1", QueueName: "victim_queue", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-1": cpu("3")}),
					}},
				},
				{
					Name: "reclaimer", QueueName: "reclaimer_queue", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZoneWithAllocatable("zone-0", cpu("4"), cpu("1")),
					nodes_fake.NewNumaZoneWithAllocatable("zone-1", cpu("4"), cpu("1")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "victim_queue", DeservedCPUs: test_utils.CreateFloat64Pointer(3000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12), ParentQueue: "d1"},
				{Name: "reclaimer_queue", DeservedCPUs: test_utils.CreateFloat64Pointer(3000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12), ParentQueue: "d1"},
			},
			Departments: []test_utils.TestDepartmentBasic{
				{Name: "d1"},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim0":   {NodeName: "node0", Status: pod_status.Releasing},
				"victim1":   {NodeName: "node0", Status: pod_status.Running, NUMAZones: []int{1}},
				"reclaimer": {NodeName: "node0", Status: pod_status.Pipelined, NUMAZones: []int{0}},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("1"), 1: cpu("1")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 5, NumberOfCacheEvictions: 1, NumberOfPipelineActions: 1,
			}},
		},
	}
}
