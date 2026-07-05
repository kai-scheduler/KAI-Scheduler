// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestHandlePreemptNuma drives the real preempt action on a single-numa-node node whose zones are
// both occupied by running Guaranteed victims, so a higher-priority pending pod can only be placed
// by evicting a victim to free a whole zone. Verifies the right (minimal) victim is evicted, the
// preemptor lands on the freed zone, and the per-zone ledger is consistent after evict+pipeline.
func TestHandlePreemptNuma(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getNumaPreemptTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.Name)

		ssn := test_utils.BuildSession(testMetadata, controller)
		preempt.New().Execute(ssn)

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

func getNumaPreemptTestsMetadata() []test_utils.TestTopologyBasic {
	return []test_utils.TestTopologyBasic{
		{
			Name: "Preemptor fits only after a zone is freed - lowest-priority victim is evicted, preemptor takes the freed zone",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					// Lower priority than victim1, so this is the one evicted.
					Name: "victim0", QueueName: "queue0", Priority: constants.PriorityTrainNumber - 1,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-0": cpu("3")}),
					}},
				},
				{
					Name: "victim1", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-1": cpu("3")}),
					}},
				},
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
			Name: "Preemptor cannot be NUMA-aligned even after eviction - no victim is preempted",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "victim0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-0": cpu("3")}),
					}},
				},
				{
					Name: "victim1", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{
						NodeName: "node0", State: pod_status.Running,
						Annotations: nodes_fake.NumaObservedPlacementAnnotation(
							map[string]map[v1.ResourceName]string{"zone-1": cpu("3")}),
					}},
				},
				{
					// Needs 5 cpu - no single 4-cpu zone can hold it even when fully freed.
					Name: "preemptor", QueueName: "queue0", Priority: constants.PriorityBuildNumber,
					RequiredCPUsPerTask: 5, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: twoFullZonesNode(),
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100000), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"victim0":   {NodeName: "node0", Status: pod_status.Running, NUMAZones: []int{0}},
				"victim1":   {NodeName: "node0", Status: pod_status.Running, NUMAZones: []int{1}},
				"preemptor": {Status: pod_status.Pending},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("1"), 1: cpu("1")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
				NumberOfCacheBinds: 0, NumberOfCacheEvictions: 0, NumberOfPipelineActions: 0,
			}},
		},
	}
}
