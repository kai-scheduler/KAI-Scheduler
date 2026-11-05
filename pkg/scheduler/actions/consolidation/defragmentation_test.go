// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"testing"
	"time"

	apiconstants "github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestDefragmentationIntervalBoundary(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	now := time.Date(2026, 3, 30, 13, 0, 0, 0, time.UTC)
	interval := time.Minute

	t.Run("runs when the interval has elapsed exactly", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := packableInferenceTopology()
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = interval
		action := consolidation.New()
		action.SetClockForTest(func() time.Time { return now }, now.Add(-interval))
		action.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})

	t.Run("skips when the interval has not elapsed", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := stayedInferenceTopology()
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = interval
		action := consolidation.New()
		action.SetClockForTest(func() time.Time { return now }, now.Add(-interval+time.Nanosecond))
		action.Execute(ssn)
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})
}

func TestDefragmentationRunsWhenPendingConsolidationIsDisabled(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	basic := packableInferenceTopology()
	ssn := test_utils.BuildSession(basic, controller)
	ssn.OverrideMaxNumberConsolidationPreemptees(0)
	ssn.SchedulerParams.DefragmentationInterval = time.Minute
	consolidation.New().Execute(ssn)
	test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
}

func TestDefragmentationFailedLookupAndIdentity(t *testing.T) {
	test_utils.InitTestingInfrastructure()

	t.Run("missing node is an error and evicts nothing", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := stayedInferenceTopology()
		basic.JobExpectedResults["running_job0"] = test_utils.TestExpectedResultBasic{
			NodeName: "missing-node", GPUsRequired: 4, Status: pod_status.Running,
		}
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = time.Minute
		for _, job := range ssn.ClusterInfo.PodGroupInfos {
			if job.Name != "running_job0" {
				continue
			}
			for _, task := range job.GetAllPodsMap() {
				task.NodeName = "missing-node"
			}
		}
		err := consolidation.New().DefragmentForTest(ssn, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing-node")
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})

	t.Run("empty podgroup UID is an error and evicts nothing", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := stayedInferenceTopology()
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = time.Minute
		for _, job := range ssn.ClusterInfo.PodGroupInfos {
			if job.Name == "running_job0" {
				job.UID = ""
			}
		}
		err := consolidation.New().DefragmentForTest(ssn, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty UID")
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})

	t.Run("empty pod UID is an error and evicts nothing", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := stayedInferenceTopology()
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = time.Minute
		for _, job := range ssn.ClusterInfo.PodGroupInfos {
			if job.Name != "running_job0" {
				continue
			}
			for _, task := range job.GetAllPodsMap() {
				task.UID = ""
			}
		}
		err := consolidation.New().DefragmentForTest(ssn, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty UID")
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})

	t.Run("negative interval is an error and evicts nothing", func(t *testing.T) {
		controller := NewController(t)
		defer controller.Finish()
		basic := stayedInferenceTopology()
		ssn := test_utils.BuildSession(basic, controller)
		ssn.SchedulerParams.DefragmentationInterval = -time.Second
		err := consolidation.New().DefragmentForTest(ssn, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "non-negative")
		test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
	})
}

func TestDefragmentationSkipsWhenSearchBudgetIsExhausted(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()
	basic := stayedInferenceTopology()
	ssn := test_utils.BuildSession(basic, controller)
	ssn.SchedulerParams.DefragmentationInterval = time.Minute
	ssn.Config.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{
			apiconstants.ActionConsolidation: {Duration: time.Nanosecond},
		},
	}
	budget, err := solvers.NewActionSearchBudget(ssn, framework.Consolidation)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)

	err = consolidation.New().DefragmentForTest(ssn, budget)
	require.NoError(t, err)
	test_utils.MatchExpectedAndRealTasks(t, 0, basic, ssn)
}

func packableInferenceTopology() test_utils.TestTopologyBasic {
	return topology(
		"packs inference jobs when the interval is due",
		[]*jobs_fake.TestJobBasic{
			inferenceJob("running_job0", 4, "node0"),
			inferenceJob("running_job1", 4, "node1"),
		},
		map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 8},
			"node1": {GPUs: 8},
		},
		8,
		map[string]test_utils.TestExpectedResultBasic{
			"running_job0": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Pipelined},
			"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
		},
		1, 1,
	)
}

func stayedInferenceTopology() test_utils.TestTopologyBasic {
	return topology(
		"leaves jobs in place",
		[]*jobs_fake.TestJobBasic{
			inferenceJob("running_job0", 4, "node0"),
			inferenceJob("running_job1", 4, "node1"),
		},
		map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 8},
			"node1": {GPUs: 8},
		},
		8,
		map[string]test_utils.TestExpectedResultBasic{
			"running_job0": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
			"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
		},
		0, 0,
	)
}

func TestDefragmentation(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	for _, tt := range defragmentationCases() {
		t.Run(tt.Name, func(t *testing.T) {
			controller := NewController(t)
			defer controller.Finish()
			ssn := test_utils.BuildSession(tt.TestTopologyBasic, controller)
			ssn.SchedulerParams.DefragmentationInterval = tt.interval
			consolidation.New().Execute(ssn)
			test_utils.MatchExpectedAndRealTasks(t, 0, tt.TestTopologyBasic, ssn)
		})
	}
}

type defragmentationCase struct {
	test_utils.TestTopologyBasic
	interval time.Duration
}

func defragmentationCases() []defragmentationCase {
	return []defragmentationCase{
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"packs inference jobs when the interval is due",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					inferenceJob("running_job1", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Pipelined},
					"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				1, 1,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"packs when the request equals idle GPUs",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					inferenceJob("running_job1", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 4},
					"node1": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Pipelined},
					"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				1, 1,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"packs a fractional request that equals idle GPUs",
				[]*jobs_fake.TestJobBasic{
					fractionalInferenceJob("running_job0", 0.5, "node0", "0"),
					fractionalInferenceJob("running_job1", 0.5, "node1", "0"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
					"node1": {GPUs: 1},
				},
				1,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName: "node1", GPUsRequired: 0.5, Status: pod_status.Pipelined, DontValidateGPUGroup: true,
					},
					"running_job1": {
						NodeName: "node1", GPUsRequired: 0.5, Status: pod_status.Running, GPUGroups: []string{"0"},
					},
				},
				1, 1,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"packs a non-preemptible job while the queue is exactly at quota",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 2, "node0"),
					inferenceJob("running_job1", 2, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				4,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node1", GPUsRequired: 2, Status: pod_status.Pipelined},
					"running_job1": {NodeName: "node1", GPUsRequired: 2, Status: pod_status.Running},
				},
				1, 1,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"does not place the moved job on an empty node",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					inferenceJob("running_job1", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
					"node2": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Pipelined},
					"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				1, 1,
			),
		},
		{
			interval: 0,
			TestTopologyBasic: topology(
				"leaves jobs in place when the interval is disabled",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					inferenceJob("running_job1", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
					"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				0, 0,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"does not move a fractional job onto a node with less idle GPU",
				[]*jobs_fake.TestJobBasic{
					fractionalInferenceJob("running_job0", 0.5, "node0", "0"),
					fractionalInferenceJob("running_job1", 0.6, "node1", "0"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 1},
					"node1": {GPUs: 1},
				},
				2,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {
						NodeName: "node0", GPUsRequired: 0.5, Status: pod_status.Running, GPUGroups: []string{"0"},
					},
					"running_job1": {
						NodeName: "node1", GPUsRequired: 0.6, Status: pod_status.Running, GPUGroups: []string{"0"},
					},
				},
				0, 0,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"does not evict when the node cannot be fully emptied",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					inferenceJob("running_job1", 4, "node0"),
					inferenceJob("running_job2", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				12,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
					"running_job1": {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
					"running_job2": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				0, 0,
			),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"does not evict a running job that shares its node with a releasing pod",
				[]*jobs_fake.TestJobBasic{
					inferenceJob("running_job0", 4, "node0"),
					releasingJob("releasing_job0", 1, "node0"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"running_job0":   {NodeName: "node0", GPUsRequired: 4, Status: pod_status.Running},
					"releasing_job0": {NodeName: "node0", GPUsRequired: 1, Status: pod_status.Releasing},
				},
				0, 0,
			),
		},
		{
			interval:          time.Minute,
			TestTopologyBasic: spanningGangCase(),
		},
		{
			interval: time.Minute,
			TestTopologyBasic: topology(
				"moves a whole gang off one node when the destination has exactly enough GPUs",
				[]*jobs_fake.TestJobBasic{
					gangJob("gang_job0", 2, "node0", "node0"),
					inferenceJob("running_job1", 4, "node1"),
				},
				map[string]nodes_fake.TestNodeBasic{
					"node0": {GPUs: 8},
					"node1": {GPUs: 8},
				},
				8,
				map[string]test_utils.TestExpectedResultBasic{
					"gang_job0":    {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Pipelined},
					"running_job1": {NodeName: "node1", GPUsRequired: 4, Status: pod_status.Running},
				},
				2, 2,
			),
		},
	}
}

func spanningGangCase() test_utils.TestTopologyBasic {
	basic := topology(
		"does not split a gang job across nodes",
		[]*jobs_fake.TestJobBasic{
			gangJob("gang_job0", 2, "node0", "node1"),
		},
		map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 4},
			"node1": {GPUs: 4},
		},
		4,
		nil,
		0, 0,
	)
	basic.TaskExpectedResults = map[string]test_utils.TestExpectedResultBasic{
		"gang_job0-0": {NodeName: "node0", GPUsRequired: 2, Status: pod_status.Running},
		"gang_job0-1": {NodeName: "node1", GPUsRequired: 2, Status: pod_status.Running},
	}
	return basic
}

func topology(
	name string,
	jobs []*jobs_fake.TestJobBasic,
	nodes map[string]nodes_fake.TestNodeBasic,
	deserved float64,
	expected map[string]test_utils.TestExpectedResultBasic,
	evictions, pipelines int,
) test_utils.TestTopologyBasic {
	basic := test_utils.TestTopologyBasic{
		Name:  name,
		Jobs:  jobs,
		Nodes: nodes,
		Queues: []test_utils.TestQueueBasic{
			{Name: "queue0", DeservedGPUs: deserved},
		},
		JobExpectedResults: expected,
		Mocks: &test_utils.TestMock{
			CacheRequirements: &test_utils.CacheMocking{},
		},
	}
	if evictions != 0 || pipelines != 0 {
		basic.Mocks.CacheRequirements.NumberOfCacheEvictions = evictions
		basic.Mocks.CacheRequirements.NumberOfPipelineActions = pipelines
	}
	return basic
}

func inferenceJob(name string, gpus float64, nodeName string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpus,
		Priority:            constants.PriorityInferenceNumber,
		QueueName:           "queue0",
		Tasks: []*tasks_fake.TestTaskBasic{
			{NodeName: nodeName, State: pod_status.Running},
		},
	}
}

func fractionalInferenceJob(name string, gpus float64, nodeName, gpuGroup string) *jobs_fake.TestJobBasic {
	job := inferenceJob(name, gpus, nodeName)
	job.Tasks[0].GPUGroups = []string{gpuGroup}
	return job
}

func releasingJob(name string, gpus float64, nodeName string) *jobs_fake.TestJobBasic {
	job := inferenceJob(name, gpus, nodeName)
	job.Tasks[0].State = pod_status.Releasing
	return job
}

func gangJob(name string, gpusPerTask float64, node0, node1 string) *jobs_fake.TestJobBasic {
	return &jobs_fake.TestJobBasic{
		Name:                name,
		RequiredGPUsPerTask: gpusPerTask,
		Priority:            constants.PriorityInferenceNumber,
		QueueName:           "queue0",
		Tasks: []*tasks_fake.TestTaskBasic{
			{NodeName: node0, State: pod_status.Running},
			{NodeName: node1, State: pod_status.Running},
		},
	}
}
