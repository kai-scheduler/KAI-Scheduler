// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const hostnameTopologyKey = "kubernetes.io/hostname"

// A terminating (Releasing) pod is still on its node. A placement that would bind now
// must keep honoring inter-pod anti-affinity against it, exactly like upstream
// kube-scheduler, while a placement that will only be Pipelined onto the releasing
// resources evaluates against the pods that will remain once it binds.
func TestAllocateWithRequiredAntiAffinityAgainstReleasingPods(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	for testNumber, testMetadata := range getAllocatePodAntiAffinityTestsMetadata() {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getAllocatePodAntiAffinityTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	preprocessLabels := map[string]string{"tier": "preprocess"}
	trainLabels := map[string]string{"tier": "train"}
	queues := []test_utils.TestQueueBasic{
		{Name: "queue0", DeservedGPUs: 4, GPUOverQuotaWeight: 1},
	}
	releasingJob := func(task *tasks_fake.TestTaskBasic) *jobs_fake.TestJobBasic {
		task.NodeName = "node0"
		task.State = pod_status.Releasing
		return &jobs_fake.TestJobBasic{
			Name:                "releasing_job0",
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue0",
			Tasks:               []*tasks_fake.TestTaskBasic{task},
		}
	}
	pendingJob := func(task *tasks_fake.TestTaskBasic) *jobs_fake.TestJobBasic {
		task.State = pod_status.Pending
		return &jobs_fake.TestJobBasic{
			Name:                "pending_job0",
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue0",
			Tasks:               []*tasks_fake.TestTaskBasic{task},
		}
	}
	node := func(gpus int) map[string]nodes_fake.TestNodeBasic {
		return map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: gpus, Labels: map[string]string{hostnameTopologyKey: "node0"}},
		}
	}
	antiAffineToPreprocess := func() *tasks_fake.TestTaskBasic {
		return &tasks_fake.TestTaskBasic{
			PodAntiAffinityLabels:      preprocessLabels,
			PodAntiAffinityTopologyKey: hostnameTopologyKey,
		}
	}
	// Labelled tier=preprocess and carrying its own required anti-affinity against
	// tier=train, so the check is the symmetric "existing pod's anti-affinity" one.
	preprocessAntiAffineToTrain := func() *tasks_fake.TestTaskBasic {
		return &tasks_fake.TestTaskBasic{
			PodAffinityLabels:          preprocessLabels,
			PodAntiAffinityLabels:      trainLabels,
			PodAntiAffinityTopologyKey: hostnameTopologyKey,
		}
	}

	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Idle GPU next to a releasing pod the pending pod is anti-affine to: stay pending, do not bind",
				Jobs: []*jobs_fake.TestJobBasic{
					releasingJob(&tasks_fake.TestTaskBasic{PodAffinityLabels: preprocessLabels}),
					pendingJob(antiAffineToPreprocess()),
				},
				Nodes:  node(2),
				Queues: queues,
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
					"pending_job0":   {GPUsRequired: 1, Status: pod_status.Pending},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Only a releasing pod the pending pod is anti-affine to blocks the node: pipeline onto it",
				Jobs: []*jobs_fake.TestJobBasic{
					releasingJob(&tasks_fake.TestTaskBasic{PodAffinityLabels: preprocessLabels}),
					pendingJob(antiAffineToPreprocess()),
				},
				Nodes:  node(1),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
					"pending_job0":   {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node0"},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Idle GPU next to a releasing pod whose own anti-affinity excludes the pending pod: stay pending",
				Jobs: []*jobs_fake.TestJobBasic{
					releasingJob(preprocessAntiAffineToTrain()),
					pendingJob(&tasks_fake.TestTaskBasic{PodAffinityLabels: trainLabels}),
				},
				Nodes:  node(2),
				Queues: queues,
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
					"pending_job0":   {GPUsRequired: 1, Status: pod_status.Pending},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Only a releasing pod whose own anti-affinity excludes the pending pod blocks the node: pipeline onto it",
				Jobs: []*jobs_fake.TestJobBasic{
					releasingJob(preprocessAntiAffineToTrain()),
					pendingJob(&tasks_fake.TestTaskBasic{PodAffinityLabels: trainLabels}),
				},
				Nodes:  node(1),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfPipelineActions: 1},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
					"pending_job0":   {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node0"},
				},
			},
		},
		{
			// Control: without anti-affinity the idle GPU is bound immediately.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Idle GPU next to a releasing pod with no anti-affinity involved: bind",
				Jobs: []*jobs_fake.TestJobBasic{
					releasingJob(&tasks_fake.TestTaskBasic{PodAffinityLabels: preprocessLabels}),
					pendingJob(&tasks_fake.TestTaskBasic{}),
				},
				Nodes:  node(2),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 1},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"releasing_job0": {GPUsRequired: 1, Status: pod_status.Releasing, NodeName: "node0"},
					"pending_job0":   {GPUsRequired: 1, Status: pod_status.Binding, NodeName: "node0"},
				},
			},
		},
	}
}
