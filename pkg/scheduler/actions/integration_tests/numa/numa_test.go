// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestNumaBookkeepingThroughFullPipeline runs the whole action pipeline
// (allocate -> consolidation -> reclaim -> preempt) on a single session and asserts the per-NUMA-zone
// ledger ends exact and non-negative.
func TestNumaBookkeepingThroughFullPipeline(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getNumaBookkeepingTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.Name)

		ssn := test_utils.BuildSession(testMetadata, controller)
		for _, action := range []framework.Action{
			allocate.New(), consolidation.New(), reclaim.New(), preempt.New(),
		} {
			action.Execute(ssn)
		}

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata, ssn)
	}
}

func cpu(amount string) map[v1.ResourceName]string {
	return map[v1.ResourceName]string{v1.ResourceCPU: amount}
}

func twoFullZonesNode() map[string]nodes_fake.TestNodeBasic {
	return map[string]nodes_fake.TestNodeBasic{
		"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
			node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
			nodes_fake.NewNumaZoneWithAllocatable("zone-0", cpu("4"), cpu("1")),
			nodes_fake.NewNumaZoneWithAllocatable("zone-1", cpu("4"), cpu("1")),
		)},
	}
}

func runningVictim(name, queue string, priority int32, zoneID string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name: name, QueueName: queue, Priority: priority,
		RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
		Tasks: []*tasks_fake.TestTaskBasic{{
			NodeName: "node0", State: pod_status.Running,
			Annotations: nodes_fake.NumaObservedPlacementAnnotation(
				map[string]map[v1.ResourceName]string{zoneID: cpu("3")}),
		}},
	}
}

func getNumaBookkeepingTestsMetadata() []test_utils.TestTopologyBasic {
	return []test_utils.TestTopologyBasic{
		{
			Name: "Preempt frees a zone - ledger stays exact through the full pipeline",
			Jobs: []*jobs_fake.TestJobBasic{
				runningVictim("victim0", "queue0", constants.PriorityTrainNumber-1, "zone-0"),
				runningVictim("victim1", "queue0", constants.PriorityTrainNumber, "zone-1"),
				{
					Name: "preemptor", QueueName: "queue0", Priority: constants.PriorityBuildNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: twoFullZonesNode(),
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim0":   {NodeName: "node0", Status: pod_status.Releasing},
				"victim1":   {NodeName: "node0", Status: pod_status.Running, NUMAZones: []int{1}},
				"preemptor": {NodeName: "node0", Status: pod_status.Pipelined, NUMAZones: []int{0}},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("1"), 1: cpu("1")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 5, NumberOfCacheEvictions: 1, NumberOfPipelineActions: 1,
			}},
		},
		{
			Name: "Reclaim frees a zone - ledger stays exact through the full pipeline",
			Jobs: []*jobs_fake.TestJobBasic{
				runningVictim("victim0", "victim_queue", constants.PriorityTrainNumber-1, "zone-0"),
				runningVictim("victim1", "victim_queue", constants.PriorityTrainNumber, "zone-1"),
				{
					Name: "reclaimer", QueueName: "reclaimer_queue", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: twoFullZonesNode(),
			Queues: []test_utils.TestQueueBasic{
				{Name: "victim_queue", DeservedCPUs: test_utils.CreateFloat64Pointer(3000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12), ParentQueue: "d1"},
				{Name: "reclaimer_queue", DeservedCPUs: test_utils.CreateFloat64Pointer(3000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12), ParentQueue: "d1"},
			},
			Departments: []test_utils.TestDepartmentBasic{{Name: "d1"}},
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
