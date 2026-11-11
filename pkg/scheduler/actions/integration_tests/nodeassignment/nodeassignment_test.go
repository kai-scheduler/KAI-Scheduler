// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeassignment_test

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

const (
	preemptible    = constants.PriorityTrainNumber
	nonPreemptible = constants.PriorityBuildNumber
)

func TestNodeAssignmentEvictions(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	integration_tests_utils.SetSchedulerActions()

	for testNumber, testData := range []integration_tests_utils.TestTopologyMetadata{
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim: owner reclaims over-quota preemptible squatters from its node",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-unassigned", "queue-b", "node-unassigned", 2, preemptible),
					running("b-squatter", "queue-b", "node-a", 2, preemptible),
					pending("a-job", "queue-a", 2, preemptible),
				},
				Nodes: twoNodes(),
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 2},
					{Name: "queue-b", DeservedGPUs: 0},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-unassigned": {NodeName: "node-unassigned", GPUsRequired: 2, Status: pod_status.Running},
					"b-squatter":   {GPUsRequired: 2, Status: pod_status.Pending},
					"a-job":        {NodeName: "node-a", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: mocks(1, 1, 1),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim: non-preemptible reclaimer only evicts pods on nodes it may use",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-unassigned", "queue-b", "node-unassigned", 2, preemptible),
					running("b-squatter", "queue-b", "node-a", 2, preemptible),
					pending("c-job", "queue-c", 2, nonPreemptible),
				},
				Nodes: twoNodes(),
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 0},
					{Name: "queue-b", DeservedGPUs: 0},
					{Name: "queue-c", DeservedGPUs: 2},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-unassigned": {GPUsRequired: 2, Status: pod_status.Pending},
					"b-squatter":   {NodeName: "node-a", GPUsRequired: 2, Status: pod_status.Running},
					"c-job":        {NodeName: "node-unassigned", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: mocks(1, 1, 1),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "reclaim: non-preemptible squatters are never evicted",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-squatter", "queue-b", "node-a", 2, nonPreemptible),
					pending("a-job", "queue-a", 2, preemptible),
				},
				Nodes: map[string]nodes_fake.TestNodeBasic{"node-a": ownedNode("queue-a")},
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 2},
					{Name: "queue-b", DeservedGPUs: 0},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-squatter": {NodeName: "node-a", GPUsRequired: 2, Status: pod_status.Running},
					"a-job":      {GPUsRequired: 2, Status: pod_status.Pending},
				},
				Mocks: mocks(0, 0, 0),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "preempt: non-preemptible pod does not evict its own queue's squatter to take an owned node",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-unassigned", "queue-b", "node-unassigned", 2, nonPreemptible),
					running("b-squatter", "queue-b", "node-a", 2, preemptible),
					pending("b-job", "queue-b", 2, nonPreemptible),
				},
				Nodes: twoNodes(),
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 0},
					{Name: "queue-b", DeservedGPUs: 4},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-unassigned": {NodeName: "node-unassigned", GPUsRequired: 2, Status: pod_status.Running},
					"b-squatter":   {NodeName: "node-a", GPUsRequired: 2, Status: pod_status.Running},
					"b-job":        {GPUsRequired: 2, Status: pod_status.Pending},
				},
				Mocks: mocks(0, 0, 0),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "consolidation: moves a preemptible pod onto an owned node to fit a non-preemptible pod",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-small", "queue-b", "node-unassigned", 1, preemptible),
					pending("b-job", "queue-b", 2, nonPreemptible),
				},
				Nodes: twoNodes(),
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 0},
					{Name: "queue-b", DeservedGPUs: 4},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-small": {NodeName: "node-a", GPUsRequired: 1, Status: pod_status.Running},
					"b-job":   {NodeName: "node-unassigned", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: mocks(2, 1, 2),
			},
		},
		{
			TestTopologyBasic: test_utils.TestTopologyBasic{
				Name: "consolidation: moves an in-quota squatter off the owned node",
				Jobs: []*jobs_fake.TestJobBasic{
					running("b-unassigned", "queue-b", "node-unassigned", 1, preemptible),
					running("b-squatter", "queue-b", "node-a", 1, preemptible),
					pending("a-job", "queue-a", 2, preemptible),
				},
				Nodes: twoNodes(),
				Queues: []test_utils.TestQueueBasic{
					{Name: "queue-a", DeservedGPUs: 2},
					{Name: "queue-b", DeservedGPUs: 2},
				},
				JobExpectedResults: map[string]test_utils.TestExpectedResultBasic{
					"b-unassigned": {NodeName: "node-unassigned", GPUsRequired: 1, Status: pod_status.Running},
					"b-squatter":   {NodeName: "node-unassigned", GPUsRequired: 1, Status: pod_status.Running},
					"a-job":        {NodeName: "node-a", GPUsRequired: 2, Status: pod_status.Running},
				},
				Mocks: mocks(2, 1, 2),
			},
		},
	} {
		t.Run(testData.TestTopologyBasic.Name, func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()
			integration_tests_utils.RunTest(t, testData, testNumber, controller)
		})
	}
}

func twoNodes() map[string]nodes_fake.TestNodeBasic {
	return map[string]nodes_fake.TestNodeBasic{
		"node-a":          ownedNode("queue-a"),
		"node-unassigned": {GPUs: 2},
	}
}

func ownedNode(queue string) nodes_fake.TestNodeBasic {
	return nodes_fake.TestNodeBasic{GPUs: 2, Labels: map[string]string{nodeassignment.DefaultLabelKey: queue}}
}

func running(name, queue, node string, gpus float64, priority int32) *jobs_fake.TestJobBasic {
	job := pending(name, queue, gpus, priority)
	job.Tasks[0] = &tasks_fake.TestTaskBasic{State: pod_status.Running, NodeName: node}
	return job
}

func pending(name, queue string, gpus float64, priority int32) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpus,
		Priority:            priority,
		QueueName:           queue,
		Tasks:               []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
	}
}

func mocks(binds, evictions, pipelines int) *test_utils.TestMock {
	return &test_utils.TestMock{
		CacheRequirements: &test_utils.CacheMocking{
			NumberOfCacheBinds:      binds,
			NumberOfCacheEvictions:  evictions,
			NumberOfPipelineActions: pipelines,
		},
		SchedulerConf: schedulerConfWithNodeAssignment(),
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
