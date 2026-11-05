// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestAllocateWithTopologySpreadConstraints(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	for testNumber, testMetadata := range getAllocateTopologySpreadTestsMetadata() {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getAllocateTopologySpreadTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	spreadLabels := map[string]string{"app": "spread"}
	spreadOverHosts := func(whenUnsatisfiable v1.UnsatisfiableConstraintAction) []v1.TopologySpreadConstraint {
		return []v1.TopologySpreadConstraint{{
			MaxSkew:           1,
			TopologyKey:       hostnameTopologyKey,
			WhenUnsatisfiable: whenUnsatisfiable,
			LabelSelector:     &metav1.LabelSelector{MatchLabels: spreadLabels},
		}}
	}
	queues := []test_utils.TestQueueBasic{
		{Name: "queue0", DeservedGPUs: 4, GPUOverQuotaWeight: 1},
	}
	runningJob := func(name, nodeName string, labels map[string]string) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name:                name,
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue0",
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:             pod_status.Running,
				NodeName:          nodeName,
				PodAffinityLabels: labels,
			}},
		}
	}
	pendingSpreadJob := func(whenUnsatisfiable v1.UnsatisfiableConstraintAction) *jobs_fake.TestJobBasic {
		return &jobs_fake.TestJobBasic{
			Name:                "pending_job0",
			RequiredGPUsPerTask: 1,
			Priority:            constants.PriorityTrainNumber,
			QueueName:           "queue0",
			Tasks: []*tasks_fake.TestTaskBasic{{
				State:                     pod_status.Pending,
				PodAffinityLabels:         spreadLabels,
				TopologySpreadConstraints: spreadOverHosts(whenUnsatisfiable),
			}},
		}
	}
	nodes := func(gpus int) map[string]nodes_fake.TestNodeBasic {
		return map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: gpus, Labels: map[string]string{hostnameTopologyKey: "node0"}},
			"node1": {GPUs: gpus, Labels: map[string]string{hostnameTopologyKey: "node1"}},
		}
	}

	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DoNotSchedule spread over hosts: place the pod on the host without a matching pod",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", spreadLabels),
					pendingSpreadJob(v1.DoNotSchedule),
				},
				Nodes:  nodes(2),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 1},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Binding, NodeName: "node1"},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "DoNotSchedule spread over hosts with room only next to the matching pod: stay pending",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", spreadLabels),
					runningJob("running_job1", "node1", nil),
					pendingSpreadJob(v1.DoNotSchedule),
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 2, Labels: map[string]string{hostnameTopologyKey: "node0"}},
					"node1": {GPUs: 1, Labels: map[string]string{hostnameTopologyKey: "node1"}},
				},
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
			// Control: ScheduleAnyway is a scoring preference, not a filter, so bin-packing still wins.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "ScheduleAnyway spread over hosts: bin-pack next to the matching pod",
				Jobs: []*jobs_fake.TestJobBasic{
					runningJob("running_job0", "node0", spreadLabels),
					pendingSpreadJob(v1.ScheduleAnyway),
				},
				Nodes:  nodes(2),
				Queues: queues,
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: 1},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"pending_job0": {GPUsRequired: 1, Status: pod_status.Binding, NodeName: "node0"},
				},
			},
		},
	}
}
