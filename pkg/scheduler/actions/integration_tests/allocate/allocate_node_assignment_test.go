// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate

import (
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/integration_tests/integration_tests_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodeassignment"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestNodeAssignmentAllocation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	integration_tests_utils.SetSchedulerActions()
	controller := NewController(t)
	defer controller.Finish()

	// Without the plugin, pods land on node-1; the expected nodes are chosen to differ from that.
	nodes := map[string]nodes_fake.TestNodeBasic{
		"node-1": {GPUs: 2, Labels: map[string]string{nodeassignment.DefaultLabelKey: "queue-b"}},
		"node-2": {GPUs: 2},
		"node-3": {GPUs: 2, Labels: map[string]string{nodeassignment.DefaultLabelKey: "queue-a"}},
	}
	queues := []test_utils.TestQueueBasic{
		{Name: "queue-a", DeservedGPUs: 2},
		{Name: "queue-b", DeservedGPUs: 2},
		{Name: "queue-c", DeservedGPUs: 2},
	}

	for testNumber, testData := range []integration_tests_utils.TestTopologyMetadata{
		{
			Name: "pod prefers a node owned by its queue",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					nodeAssignmentPendingJob("job-a", "queue-a"),
				},
				Nodes:  nodes,
				Queues: queues,
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-a": {NodeName: "node-3", GPUsRequired: 1, Status: pod_status.Running},
				},
				Mocks: nodeAssignmentMocks(1),
			},
		},
		{
			Name: "pod of a queue without nodes prefers an unassigned node",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					nodeAssignmentPendingJob("job-c", "queue-c"),
				},
				Nodes:  nodes,
				Queues: queues,
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"job-c": {NodeName: "node-2", GPUsRequired: 1, Status: pod_status.Running},
				},
				Mocks: nodeAssignmentMocks(1),
			},
		},
		{
			Name: "pod uses another queue's node when unassigned nodes are full",
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Jobs: []*jobs_fake.TestJobBasic{
					nodeAssignmentRunningJob("running-c", "queue-c", "node-2", 2),
					nodeAssignmentPendingJob("job-c", "queue-c"),
				},
				Nodes:  nodes,
				Queues: queues,
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"running-c": {NodeName: "node-2", GPUsRequired: 2, Status: pod_status.Running},
					"job-c":     {GPUsRequired: 1, Status: pod_status.Running},
				},
				Mocks: nodeAssignmentMocks(1),
			},
		},
	} {
		t.Run(testData.Name, func(t *testing.T) {
			integration_tests_utils.RunTest(t, testData, testNumber, controller)
		})
	}
}

func nodeAssignmentPendingJob(name, queue string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: 1,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           queue,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	}
}

func nodeAssignmentRunningJob(name, queue, node string, gpus float64) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpus,
		Priority:            constants.PriorityTrainNumber,
		QueueName:           queue,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Running, NodeName: node}},
	}
}

func nodeAssignmentMocks(binds int) *test_utils.TestMock {
	return &test_utils.TestMock{
		CacheRequirements: &test_utils.CacheMocking{NumberOfCacheBinds: binds},
		SchedulerConf:     schedulerConfWithNodeAssignment(),
	}
}

func schedulerConfWithNodeAssignment() *conf.SchedulerConfiguration {
	config, err := conf_util.GetDefaultSchedulerConf()
	if err != nil {
		panic(err)
	}
	config.ScenarioSearchBudgets = nil
	config.Tiers[0].Plugins = append(config.Tiers[0].Plugins, conf.PluginOption{Name: nodeassignment.Name})
	return config
}
