// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim_test

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/reclaim"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

const hostnameTopologyKey = "kubernetes.io/hostname"

// A pending job with a required pod anti-affinity against the running victims must
// still be reclaimable: evicted victims become Releasing, and a Releasing pod must not
// keep satisfying the anti-affinity selector or every reclaim scenario is rejected.
func TestReclaimWithRequiredAntiAffinityAgainstVictims(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	for testNumber, testMetadata := range getReclaimPodAntiAffinityTestsMetadata() {
		t.Logf("Running test number: %v, test name: %v,", testNumber, testMetadata.TestTopologyBasic.Name)
		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		reclaimAction := reclaim.New()
		reclaimAction.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getReclaimPodAntiAffinityTestsMetadata() []integration_tests_utils.TestTopologyMetadata {
	preprocessLabels := map[string]string{"tier": "preprocess"}
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Reclaim victims the pending job has a required anti-affinity against, and pipeline onto their node",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:          "node0",
								State:             pod_status.Running,
								PodAffinityLabels: preprocessLabels,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:                      pod_status.Pending,
								PodAntiAffinityLabels:      preprocessLabels,
								PodAntiAffinityTopologyKey: hostnameTopologyKey,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:   2,
						Labels: map[string]string{hostnameTopologyKey: "node0"},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
					{Name: "queue1", DeservedGPUs: 2, GPUOverQuotaWeight: 2},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Releasing},
					"pending_job0": {GPUsRequired: 2, Status: pod_status.Pipelined, NodeName: "node0"},
				},
			},
		},
		{
			// Control: a matching pod that is NOT a victim (its queue is within quota) keeps
			// blocking the node. The fix only ignores Releasing pods, not every match.
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Do not reclaim when a non-victim pod still matches the required anti-affinity",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:          "node0",
								State:             pod_status.Running,
								PodAffinityLabels: preprocessLabels,
							},
						},
					},
					{
						Name:                "running_job1",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue2",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName:          "node0",
								State:             pod_status.Running,
								PodAffinityLabels: preprocessLabels,
							},
						},
					},
					{
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State:                      pod_status.Pending,
								PodAntiAffinityLabels:      preprocessLabels,
								PodAntiAffinityTopologyKey: hostnameTopologyKey,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs:   3,
						Labels: map[string]string{hostnameTopologyKey: "node0"},
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue0", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
					{Name: "queue1", DeservedGPUs: 2, GPUOverQuotaWeight: 2},
					{Name: "queue2", DeservedGPUs: 2, GPUOverQuotaWeight: 2},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  0,
						NumberOfPipelineActions: 0,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"running_job1": {GPUsRequired: 1, Status: pod_status.Running, NodeName: "node0"},
					"pending_job0": {GPUsRequired: 2, Status: pod_status.Pending},
				},
			},
		},
	}
}
