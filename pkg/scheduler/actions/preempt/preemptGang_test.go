// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt_test

import (
	"fmt"
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/actions/preempt"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestHandleGangPreempt(t *testing.T) {

	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getTestsGangMetadata()
	for testNumber, testMetadata := range testsMetadata {
		t.Logf("Running Test %d: %s", testNumber, testMetadata.TestTopologyBasic.Name)

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTestsGangMetadata() []integration_tests_utils.TestTopologyMetadata {
	return []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Don't preempt if not all tasks can be allocated",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 1,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:               "queue0",
						DeservedGPUs:       2,
						GPUOverQuotaWeight: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "Preempt if all tasks can be allocated",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 2,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 2,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						NodeName:     "node0",
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheBinds:      1,
						NumberOfCacheEvictions:  1,
						NumberOfPipelineActions: 2,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "don't preempt if will go over quota",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 1,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 1,
						Status:       pod_status.Running,
					},
					"pending_job0": {
						GPUsRequired: 4,
						Status:       pod_status.Pending,
					},
				},
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "preempt gang job",
				Jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "running_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityTrainNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
							{
								NodeName: "node0",
								State:    pod_status.Running,
							},
						},
					}, {
						Name:                "pending_job0",
						RequiredGPUsPerTask: 2,
						Priority:            constants.PriorityBuildNumber,
						QueueName:           "queue0",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								State: pod_status.Pending,
							},
						},
					},
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{
					"node0": {
						GPUs: 4,
					},
				},
				Queues: []test_utils.TestQueueBasic{
					{
						Name:         "queue0",
						DeservedGPUs: 3,
					},
				},
				Mocks: &test_utils.TestMock{
					CacheRequirements: &test_utils.CacheMocking{
						NumberOfCacheEvictions:  2,
						NumberOfPipelineActions: 1,
					},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName:     "node0",
						GPUsRequired: 4,
						Status:       pod_status.Releasing,
					},
					"pending_job0": {
						GPUsRequired: 2,
						Status:       pod_status.Pipelined,
					},
				},
			},
		},
	}
}

func TestHandleScatteredNodesForGangPreempt(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	testsMetadata := getTestsScatteredNodesForGangMetadata()
	for testNumber, testMetadata := range testsMetadata {

		ssn := test_utils.BuildSession(testMetadata.TestTopologyBasic, controller)
		preemptAction := preempt.New()
		preemptAction.Execute(ssn)

		test_utils.MatchExpectedAndRealTasks(t, testNumber, testMetadata.TestTopologyBasic, ssn)
	}
}

func getTestsScatteredNodesForGangMetadata() []integration_tests_utils.TestTopologyMetadata {
	const (
		nodeCount  = 4
		gpuPerNode = 1
		queueName  = "queue"
	)

	test := integration_tests_utils.TestTopologyMetadata{
		TestTopologyBasic: test_utils.TestTopologyBasic{
			Name:  "flaky scenario",
			Jobs:  make([]*jobs_fake.TestJobBasic, 0),
			Nodes: map[string]nodes_fake.TestNodeBasic{},
			Queues: []test_utils.TestQueueBasic{
				{
					Name:               queueName,
					MaxAllowedGPUs:     -1,
					GPUOverQuotaWeight: 1,
				},
			},
			JobExpectedResults: make(map[string]test_utils.TestExpectedResultBasic),
			Mocks: &test_utils.TestMock{
				CacheRequirements: &test_utils.CacheMocking{
					NumberOfCacheBinds:      2,
					NumberOfCacheEvictions:  200,
					NumberOfPipelineActions: 2,
				},
			},
		},
	}

	var nodeNames []string
	test.Name = "flaky scenario"
	for i := 0; i < nodeCount; i++ {
		nodeName := fmt.Sprintf("node%d", i)
		node := nodes_fake.TestNodeBasic{
			GPUs: gpuPerNode,
		}
		test.Nodes[nodeName] = node
		nodeNames = append(nodeNames, nodeName)

		for j := 0; j < gpuPerNode; j++ {
			jobName := fmt.Sprintf("job-%d-%d", i, j)
			job := &jobs_fake.TestJobBasic{
				Name:                jobName,
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           queueName,
				JobAgeInMinutes:     i*gpuPerNode + j,
				Tasks: []*tasks_fake.TestTaskBasic{
					{
						State:    pod_status.Running,
						NodeName: nodeName,
					},
				},
			}
			test.Jobs = append(test.Jobs, job)
		}
	}

	test.Jobs = append(test.Jobs, &jobs_fake.TestJobBasic{
		RequiredGPUsPerTask: 1,
		Priority:            constants.PriorityInteractivePreemptibleNumber,
		Name:                "preemptor",
		QueueName:           queueName,
		Tasks: []*tasks_fake.TestTaskBasic{
			{
				State:             pod_status.Pending,
				NodeAffinityNames: []string{nodeNames[1]},
			},
			{
				State:             pod_status.Pending,
				NodeAffinityNames: []string{nodeNames[3]},
			},
		},
	})
	test.JobExpectedResults["preemptor"] = test_utils.TestExpectedResultBasic{
		GPUsRequired:         2,
		Status:               pod_status.Pipelined,
		DontValidateGPUGroup: true,
	}
	return []integration_tests_utils.TestTopologyMetadata{test}
}
