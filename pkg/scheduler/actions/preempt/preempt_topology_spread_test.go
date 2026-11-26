// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"testing"

	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const hostnameTopologyKey = "kubernetes.io/hostname"

func TestPreemptWithTopologySpreadConstraints(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	spreadLabels := map[string]string{"app": "spread"}
	testMetadata := test_utils.TestTopologyBasic{
		Name: "Lower-priority matching pod that the simulation would re-place next to the pending pod: DoNotSchedule spread does not preempt",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "running_job0",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State:             pod_status.Running,
					NodeName:          "node0",
					PodAffinityLabels: spreadLabels,
				}},
			},
			{
				Name:                "running_job1",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityBuildNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State:    pod_status.Running,
					NodeName: "node1",
				}},
			},
			{
				Name:                "pending_job0",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityBuildNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State:             pod_status.Pending,
					PodAffinityLabels: spreadLabels,
					TopologySpreadConstraints: []v1.TopologySpreadConstraint{{
						MaxSkew:           1,
						TopologyKey:       hostnameTopologyKey,
						WhenUnsatisfiable: v1.DoNotSchedule,
						LabelSelector:     &metav1.LabelSelector{MatchLabels: spreadLabels},
					}},
				}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 2, Labels: map[string]string{hostnameTopologyKey: "node0"}},
			"node1": {GPUs: 1, Labels: map[string]string{hostnameTopologyKey: "node1"}},
		},
		Queues: []test_utils.TestQueueBasic{{Name: "queue0", DeservedGPUs: 3}},
		Mocks:  &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
		JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
			"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
			"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node1"},
			"pending_job0": {GPUsRequired: 1, Status: pod_status.Pending},
		},
	}

	ssn := test_utils.BuildSession(testMetadata, controller)
	preempt.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, testMetadata, ssn)
}
