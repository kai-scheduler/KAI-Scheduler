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

func TestHandleAllocationNumaReleasing(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	testMetadata := test_utils.TestTopologyBasic{
		Name: "Pending pod is pipelined onto the NUMA zone of a terminating pod",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name: "releasing_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
				RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node0", State: pod_status.Releasing,
					Annotations: nodes_fake.NumaObservedPlacementAnnotation(
						map[string]map[v1.ResourceName]string{"zone-0": cpu("3")}),
				}},
			},
			{
				Name: "running_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
				RequiredCPUsPerTask: 3, QOSClass: v1.PodQOSGuaranteed,
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node0", State: pod_status.Running,
					Annotations: nodes_fake.NumaObservedPlacementAnnotation(
						map[string]map[v1.ResourceName]string{"zone-1": cpu("3")}),
				}},
			},
			{
				Name: "pending_job0", QueueName: "queue0", Priority: constants.PriorityTrainNumber,
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
			{Name: "queue0", DeservedCPUs: test_utils.CreateFloat64Pointer(100), DeservedMemory: test_utils.CreateFloat64Pointer(1e12)},
		},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"releasing_job0": {NodeName: "node0", Status: pod_status.Releasing},
			"running_job0":   {NodeName: "node0", Status: pod_status.Running, NUMAZones: []int{1}},
			"pending_job0":   {NodeName: "node0", Status: pod_status.Pipelined, NUMAZones: []int{0}},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheBinds: 0, NumberOfPipelineActions: 1,
		}},
	}

	ssn := test_utils.BuildSession(testMetadata, controller)
	allocate.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, testMetadata, ssn)
}
