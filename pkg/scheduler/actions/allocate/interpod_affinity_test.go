// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestInterpodAffinityPredicate(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	for testNumber, testMetadata := range getPodAffinityTestsMetadata() {
		t.Logf("Running test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		allocateAction := allocate.New()
		allocateAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getPodAffinityTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pod anti affinity scheduling",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "affinity-job",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:                      pod_status.Pending,
								PodAffinityLabels:          map[string]string{"app": "database"},
								PodAffinityTopologyKey:     "topology.kubernetes.io/zone",
								PodAntiAffinityTopologyKey: tasks_fake.NodeAffinityKey,
							},
						},
					},
					{
						Name:                "existing-database",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						// Simulate existing pod with matching affinity label
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Running,
								NodeName:          "node1",
								PodAffinityLabels: map[string]string{"app": "database"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"affinity-job": {
						GPUsRequired: 1,
						Status:       pod_status.Binding,
						NodeName:     "node0", // Should schedule on node0 due to pod anti affinity
					},
					"existing-database": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pod affinity and anti-affinity scheduling makes job stay pending",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "affinity-job",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:                      pod_status.Pending,
								PodAffinityLabels:          map[string]string{"app": "database"},
								PodAffinityTopologyKey:     "topology.kubernetes.io/zone",
								PodAntiAffinityTopologyKey: tasks_fake.NodeAffinityKey,
							},
						},
					},
					{
						Name:                "existing-database",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						// Simulate existing pod with matching affinity label
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Running,
								NodeName:          "node1",
								PodAffinityLabels: map[string]string{"app": "database"},
							},
						},
					},
					{
						Name:                "existing-web-server",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						// Simulate existing pod with affinity label
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Running,
								NodeName:          "node0",
								PodAffinityLabels: map[string]string{"app": "database"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
					"node1": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
					"node2": {
						GPUs: 2,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "west",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"affinity-job": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"existing-database": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"existing-web-server": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Pod affinity condition makes job stay pending",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "affinity-job",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:                  pod_status.Pending,
								PodAffinityLabels:      map[string]string{"app": "database"},
								PodAffinityTopologyKey: "topology.kubernetes.io/zone",
							},
						},
					},
					{
						Name:                "existing-database",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						// Simulate existing pod with matching affinity label
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Running,
								NodeName:          "node1",
								PodAffinityLabels: map[string]string{"app": "database"},
							},
						},
					},
					{
						Name:                "existing-web-server",
						QueueName:           "queue0",
						RequiredGPUsPerTask: 1,
						// Simulate existing pod with affinity label
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:             pod_status.Running,
								NodeName:          "node0",
								PodAffinityLabels: map[string]string{"app": "database"},
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
					"node1": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "east",
						},
					},
					"node2": {
						GPUs: 1,
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "west",
						},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"affinity-job": {
						GPUsRequired: 1,
						Status:       pod_status.Pending,
					},
					"existing-database": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"existing-web-server": {
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds: 1,
					},
				},
			},
		},
	}
}
