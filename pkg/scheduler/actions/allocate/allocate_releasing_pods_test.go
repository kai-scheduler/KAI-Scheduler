// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

// Anti-affinity counts are aggregated per topology domain, so a releasing pod blocks every node of
// its rack, not only its own. Pipelining must evaluate the rack without it.
func TestAllocateWithRackAntiAffinityAgainstReleasingPodOnAnotherNode(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	const rackTopologyKey = "rack"
	preprocessLabels := map[string]string{"tier": "preprocess"}
	testMetadata := test_utils.TestTopologyBasic{
		Name: "Free node in the rack of a releasing anti-affine pod on another node: pipeline",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:      "releasing_job0",
				Priority:  constants.PriorityTrainNumber,
				QueueName: "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName:          "node1",
					State:             pod_status.Releasing,
					PodAffinityLabels: preprocessLabels,
				}},
			},
			{
				Name:                "pending_job0",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State:                      pod_status.Pending,
					PodAntiAffinitySelector:    preprocessLabels,
					PodAntiAffinityTopologyKey: rackTopologyKey,
				}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1, Labels: map[string]string{hostnameTopologyKey: "node0", rackTopologyKey: "r1"}},
			"node1": {GPUs: 0, Labels: map[string]string{hostnameTopologyKey: "node1", rackTopologyKey: "r1"}},
		},
		Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 1, GPUOverQuotaWeight: 1}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1}},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"releasing_job0": {Status: pod_status.Releasing, NodeName: "node1"},
			"pending_job0":   {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node0"},
		},
	}

	ssn := test_utils.BuildSession(testMetadata, controller)
	allocate.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, testMetadata, ssn)
}

func TestAllocateWithHostPortOfReleasingPod(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range []test_utils.TestTopologyBasic{
		{
			Name: "Host port held by a releasing pod: pipeline, do not bind",
			Jobs: []*jobs_fake.TestJobBasic{
				hostPortJob("holder_job0", pod_status.Releasing, "node0"),
				hostPortJob("pending_job0", pod_status.Pending, ""),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}},
			Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2, GPUOverQuotaWeight: 1}},
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
				"pending_job0": {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node0"},
			},
		},
		{
			// Control: a running pod keeps its port.
			Name: "Host port held by a running pod: stay pending",
			Jobs: []*jobs_fake.TestJobBasic{
				hostPortJob("holder_job0", pod_status.Running, "node0"),
				hostPortJob("pending_job0", pod_status.Pending, ""),
			},
			Nodes:  map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 2}},
			Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 2, GPUOverQuotaWeight: 1}},
			Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
			JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
				"holder_job0":  {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
				"pending_job0": {GPUsRequired: 1, Status: pod_status.Pending},
			},
		},
	} {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.Name)
		ssn := test_utils.BuildSession(testMetadata, controller)
		allocate.New().Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata, ssn)
	}
}

func hostPortJob(name string, state pod_status.PodStatus, nodeName string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: 1,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           "queue0",
		Tasks: []*tasks_fake.TestTaskBasic{{
			State:     state,
			NodeName:  nodeName,
			HostPorts: []int32{8080},
		}},
	}
}
