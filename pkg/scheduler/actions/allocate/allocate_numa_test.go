// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// TestHandleAllocationNuma drives the real allocate action against single-numa-node and restricted
// nodes, asserting both the scheduling verdict (which node, or stays pending) and the resulting
// per-zone NUMA placement. The numa plugin is enabled automatically because nodes carry a topology.
func TestHandleAllocationNuma(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getNumaAllocationTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.Name)

		ssn := test_utils.BuildSession(testMetadata, controller)
		allocate.New().Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata, ssn)
	}
}

func cpu(amount string) map[v1.ResourceName]string {
	return map[v1.ResourceName]string{v1.ResourceCPU: amount}
}

func getNumaAllocationTestsMetadata() []test_utils.TestTopologyBasic {
	return []test_utils.TestTopologyBasic{
		{
			Name: "Guaranteed pod fits a single NUMA zone - placed on the lowest fitting zone",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {NodeName: "node0", Status: pod_status.Binding, NUMAZones: []int{0}},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("1"), 1: cpu("4")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 5}},
		},
		{
			Name: "Guaranteed pod too large for any single zone - stays pending (NUMA, not capacity)",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 6, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {Status: pod_status.Pending},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("4"), 1: cpu("4")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 0}},
		},
		{
			Name: "Node selection - lands on the node that can NUMA-align, skips the one whose zones are too small",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"small-node": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("2")), nodes_fake.NewNumaZone("zone-1", cpu("2")),
				)},
				"good-node": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {NodeName: "good-node", Status: pod_status.Binding, NUMAZones: []int{0}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 5}},
		},
		{
			Name: "In-cycle per-zone accounting - two pods fill both zones, third has no zone left",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "high", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
				{
					Name: "mid", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
				{
					Name: "low", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"high": {NodeName: "node0", Status: pod_status.Binding, NUMAZones: []int{0}},
				"mid":  {NodeName: "node0", Status: pod_status.Binding, NUMAZones: []int{1}},
				"low":  {Status: pod_status.Pending},
			},
			ExpectedNodesResources: map[string]test_utils.TestExpectedNodesResources{
				"node0": {NUMAZonesAvailable: map[int]map[v1.ResourceName]string{0: cpu("1"), 1: cpu("1")}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 5}},
		},
		{
			Name: "Restricted policy - request spanning two zones is admitted and split across the mask",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 6, QOSClass: v1.PodQOSGuaranteed,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicyRestricted, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {NodeName: "node0", Status: pod_status.Binding, NUMAZones: []int{0, 1}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 5}},
		},
		{
			Name: "Pass-through - a Burstable pod is not NUMA-constrained even when it fits no single zone",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
					RequiredCPUsPerTask: 6, QOSClass: v1.PodQOSBurstable,
					Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {CPUMillis: 1000, CPUMemory: 1e12, NumaTopology: nodes_fake.NewNumaTopology(
					node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopePod,
					nodes_fake.NewNumaZone("zone-0", cpu("4")), nodes_fake.NewNumaZone("zone-1", cpu("4")),
				)},
			},
			Queues: []test_utils.TestQueueBasic{
				{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
			},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"pending_job0": {NodeName: "node0", Status: pod_status.Binding, NUMAZones: []int{}},
			},
			Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 5}},
		},
	}
}
