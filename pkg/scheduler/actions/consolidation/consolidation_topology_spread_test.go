// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const hostnameTopologyKey = "kubernetes.io/hostname"

func TestConsolidationWithTopologySpreadConstraints(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	for testNumber, testMetadata := range getConsolidationTopologySpreadTestsMetadata() {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		consolidation.New().Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getConsolidationTopologySpreadTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	spreadLabels := map[string]string{"app": "spread"}
	spreadOverHosts := func(whenUnsatisfiable v1.UnsatisfiableConstraintAction) []v1.TopologySpreadConstraint {
		return []v1.TopologySpreadConstraint{{
			MaxSkew:           1,
			TopologyKey:       hostnameTopologyKey,
			WhenUnsatisfiable: whenUnsatisfiable,
			LabelSelector:     &metav1.LabelSelector{MatchLabels: spreadLabels},
		}}
	}
	runningJob := func(name, nodeName string, gpus float64, priority int32, labels map[string]string) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name:                name,
			RequiredGPUsPerTask: gpus,
			Priority:            priority,
			QueueName:           "queue0",
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:             pod_status.Running,
				NodeName:          nodeName,
				PodAffinityLabels: labels,
			}},
		}
	}
	pendingSpreadJob := func(gpus float64, whenUnsatisfiable v1.UnsatisfiableConstraintAction) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name:                "pending_job0",
			RequiredGPUsPerTask: gpus,
			Priority:            constants.PriorityInteractivePreemptibleNumber,
			QueueName:           "queue0",
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:                     pod_status.Pending,
				PodAffinityLabels:         spreadLabels,
				TopologySpreadConstraints: spreadOverHosts(whenUnsatisfiable),
			}},
		}
	}
	nodes := func(node0GPUs, node1GPUs int) map[string]nodes_fake.TestNodeBasic {
		return map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: node0GPUs, Labels: map[string]string{hostnameTopologyKey: "node0"}},
			"node1": {GPUs: node1GPUs, Labels: map[string]string{hostnameTopologyKey: "node1"}},
		}
	}
	queues := []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 4}}

	movedVictimCase := func(name string, whenUnsatisfiable v1.UnsatisfiableConstraintAction) integration_tests_utils.TestTopologyMetadata {
		return integration_tests_utils.TestTopologyMetadata{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: name,
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", 1, constants.PriorityTrainNumber, nil),
					runningJob("running_job1", "node1", 1, constants.PriorityBuildNumber, nil),
					pendingSpreadJob(2, whenUnsatisfiable),
				},
				Nodes:  nodes(2, 2),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node1"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node1"},
					"pending_job0": {GPUsRequired: 2, Status: pod_status.Pipelined, NodeName: "node0"},
				},
			},
		}
	}

	return []integration_tests_utils.TestTopologyMetadata{
		movedVictimCase("Control, no matching pod: DoNotSchedule spread over hosts consolidates the victim away",
			v1.DoNotSchedule),
		movedVictimCase("Control, ScheduleAnyway spread consolidates the victim away", v1.ScheduleAnyway),
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preemptible matching pod that the simulation would put back on the pending pod's node: DoNotSchedule spread does not consolidate",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", 1, constants.PriorityTrainNumber, spreadLabels),
					runningJob("running_job1", "node1", 1, constants.PriorityBuildNumber, nil),
					pendingSpreadJob(1, v1.DoNotSchedule),
				},
				Nodes:  nodes(2, 1),
				Queues: queues,
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node1"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Pending},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Control, same layout with ScheduleAnyway spread: consolidates",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", 1, constants.PriorityTrainNumber, spreadLabels),
					runningJob("running_job1", "node1", 1, constants.PriorityBuildNumber, nil),
					pendingSpreadJob(1, v1.ScheduleAnyway),
				},
				Nodes:  nodes(2, 1),
				Queues: queues,
				Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{NumberOfCacheEvictions: 1, NumberOfPipelineActions: 2}},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node1"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Pipelined, NodeName: "node0"},
				},
			},
		},
	}
}
