// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestNewJobsSolverDefaultsNilBudgetToUnlimited(t *testing.T) {
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil)

	require.NotNil(t, solver.actionBudget)
	require.False(t, solver.actionBudget.Exhausted())
	require.Greater(t, solver.actionBudget.BeginJob().Remaining(), time.Hour)
}

func TestSolveWithResultReturnsTerminalResultWhenNoTasksToAllocate(t *testing.T) {
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil)
	pendingJob := podgroup_info.NewPodGroupInfo("pending-job")

	solved, statement, victims, result := solver.SolveWithResult(&framework.Session{}, pendingJob)

	require.False(t, solved)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, SearchResultGeneratorsExhausted, result.Reason())
	require.False(t, result.ReducedBudget())
}

func TestSolveWithResultRecordsNoSearchMetricAsNotAttempted(t *testing.T) {
	labels := map[string]string{
		"action":         "reclaim",
		"result":         string(SearchResultNotAttempted),
		"reduced_budget": "false",
	}
	before := scenarioSearchCounterValue(t, "scenario_search_jobs_total", labels)
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil)
	pendingJob := podgroup_info.NewPodGroupInfo("pending-job")

	_, _, _, result := solver.SolveWithResult(&framework.Session{}, pendingJob)

	require.Equal(t, SearchResultGeneratorsExhausted, result.Reason())
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_jobs_total", labels))
}

func TestSolveWithResultReturnsNoGeneratorWhenGeneratorFuncIsNil(t *testing.T) {
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil)

	solved, statement, victims, result := solver.SolveWithResult(ssn, pendingJob)

	require.False(t, solved)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, SearchResultNoGenerator, result.Reason())
	require.False(t, result.ReducedBudget())
}

func TestSolveWithResultReturnsNoGeneratorWhenGeneratorReturnsNil(t *testing.T) {
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	solver := NewJobsSolver(
		nil,
		nil,
		func() *utils.JobsOrderByQueues {
			return nil
		},
		framework.Reclaim,
		nil,
	)

	solved, statement, victims, result := solver.SolveWithResult(ssn, pendingJob)

	require.False(t, solved)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, SearchResultNoGenerator, result.Reason())
	require.False(t, result.ReducedBudget())
}

func TestSolveWithResultUsesMinJobBudgetAfterActionBudgetExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("10ms"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MinJobSearchDuration: scenarioSearchDurationPtrForTest("50ms"),
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, actionBudget)

	clock.Advance(10 * time.Millisecond)
	solved, statement, victims, result := solver.SolveWithResult(ssn, pendingJob)

	require.False(t, solved)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, SearchResultNoGenerator, result.Reason())
	require.True(t, result.ReducedBudget())
}

func TestSolveWithResultReportsDeadlineWhenBudgetExhaustsDuringScenarioSearch(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("10ms"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1ms"),
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	node := node_info.NewNodeInfo(
		common_info.BuildNode("node-0", common_info.BuildResourceList("4", "16Gi")),
		nil, resource_info.NewResourceVectorMap(),
	)
	ssn.ClusterInfo.Nodes[node.Name] = node
	ssn.AddScenarioGenerator("deadline-test", func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		solveCtx.GenerateVictimsQueue()
		return &portfolioTestGenerator{name: "deadline-test"}
	})
	solver := NewJobsSolver(
		[]*node_info.NodeInfo{node},
		nil,
		func() *utils.JobsOrderByQueues {
			clock.Advance(time.Millisecond)
			return utils.GetVictimsQueue(ssn, nil)
		},
		framework.Reclaim,
		actionBudget,
	)

	solved, statement, victims, result := solver.SolveWithResult(ssn, pendingJob)

	require.False(t, solved)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, SearchResultDeadlineExhausted, result.Reason())
}

func TestSolveWithResultRecordsGeneratorExhaustedMetricAfterGeneratorAttempt(t *testing.T) {
	labels := map[string]string{
		"action":         "reclaim",
		"result":         string(SearchResultGeneratorsExhausted),
		"reduced_budget": "false",
	}
	before := scenarioSearchCounterValue(t, "scenario_search_jobs_total", labels)
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	ssn.AddScenarioGenerator("empty", portfolioTestFactory(&portfolioTestGenerator{name: "empty"}))
	solver := NewJobsSolver(
		nil,
		nil,
		func() *utils.JobsOrderByQueues {
			return utils.GetVictimsQueue(ssn, nil)
		},
		framework.Reclaim,
		nil,
	)

	_, _, _, result := solver.SolveWithResult(ssn, pendingJob)

	require.Equal(t, SearchResultGeneratorsExhausted, result.Reason())
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_jobs_total", labels))
}

func TestSolveWithResultRecordsUnsolvedScenarioDurationAfterSimulation(t *testing.T) {
	generatorName := "test-unsolved-duration"
	labels := map[string]string{
		"action":    "reclaim",
		"generator": generatorName,
		"result":    scenarioSearchResultUnsolved,
	}
	before := scenarioSearchHistogramCount(t, "scenario_search_duration_seconds", labels)
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	ssn.ClusterInfo.Nodes = map[string]*node_info.NodeInfo{"node-1": {}}
	scenarioToSolve := scenario.NewByNodeScenario(
		ssn, pendingJob,
		podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false),
		nil, nil,
	)
	ssn.AddScenarioGenerator(generatorName, portfolioTestFactory(&portfolioTestGenerator{
		name:      generatorName,
		scenarios: []api.ScenarioInfo{scenarioToSolve},
	}))
	solver := NewJobsSolver(
		nil,
		nil,
		func() *utils.JobsOrderByQueues {
			return utils.GetVictimsQueue(ssn, nil)
		},
		framework.Reclaim,
		nil,
	)

	solver.SolveWithResult(ssn, pendingJob)

	require.Equal(t, before+1, scenarioSearchHistogramCount(t, "scenario_search_duration_seconds", labels))
}

func TestSolveWithResultRunsCompletePartialSearchForOneGeneratorBeforeNext(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{
		"node-1": 1,
		"node-2": 1,
		"node-3": 1,
	})
	require.NoError(t, ssn.InitNodeScoringPool())
	pendingJob := addGeneratorTestPendingJob(t, ssn, 3, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 3)
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 3, 20, "team-victim", "node-1", "node-2", "node-3")
	factoryCalls := []string{}

	ssn.AddScenarioGenerator("first", func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		factoryCalls = append(factoryCalls, fmt.Sprintf("first:%d", solveCtx.ProbeK))
		return &portfolioTestGenerator{name: "first"}
	})
	ssn.AddScenarioGenerator("second", func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		factoryCalls = append(factoryCalls, fmt.Sprintf("second:%d", solveCtx.ProbeK))
		pendingTasks := podgroup_info.GetTasksToAllocate(
			solveCtx.PartialPendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false,
		)
		sn := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks,
			unrecordedVictimsForProbe(victimTasks, solveCtx.RecordedVictimsTasks, solveCtx.ProbeK),
			solveCtx.RecordedVictimsJobs,
		)
		return &portfolioTestGenerator{name: "second", scenarios: []api.ScenarioInfo{sn}}
	})
	solver := NewJobsSolver(
		jobSolverResultTestFeasibleNodes(ssn),
		nil,
		generatorTestVictimsQueueFactory(ssn, victimJob),
		framework.Reclaim,
		nil,
	)

	solved, statement, _, result := solver.SolveWithResult(ssn, pendingJob)
	if statement != nil {
		defer statement.Discard()
	}

	require.True(t, solved)
	require.Equal(t, SearchResultSolved, result.Reason())
	require.Equal(t, []string{"first:1", "second:1", "second:2", "second:3", "second:3"}, factoryCalls)
}

func TestSolveWithResultStillSolvesWhenGeneratorRepeatsScenarios(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{
		"node-1": 1,
		"node-2": 1,
		"node-3": 1,
	})
	require.NoError(t, ssn.InitNodeScoringPool())
	pendingJob := addGeneratorTestPendingJob(t, ssn, 3, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 3)
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 3, 20, "team-victim", "node-1", "node-2", "node-3")
	generatorName := "dedup-e2e"

	ssn.AddScenarioGenerator(generatorName, func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		pendingTasks := podgroup_info.GetTasksToAllocate(
			solveCtx.PartialPendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false,
		)
		failing := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks, nil, solveCtx.RecordedVictimsJobs,
		)
		failingDuplicate := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks, nil, solveCtx.RecordedVictimsJobs,
		)
		solving := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks,
			unrecordedVictimsForProbe(victimTasks, solveCtx.RecordedVictimsTasks, solveCtx.ProbeK),
			solveCtx.RecordedVictimsJobs,
		)
		return &portfolioTestGenerator{
			name:      generatorName,
			scenarios: []api.ScenarioInfo{failing, failingDuplicate, solving},
		}
	})
	labels := map[string]string{"action": "reclaim", "generator": generatorName, "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)
	solver := NewJobsSolver(
		jobSolverResultTestFeasibleNodes(ssn),
		nil,
		generatorTestVictimsQueueFactory(ssn, victimJob),
		framework.Reclaim,
		nil,
	)

	solved, statement, _, result := solver.SolveWithResult(ssn, pendingJob)
	if statement != nil {
		defer statement.Discard()
	}

	require.True(t, solved)
	require.NotNil(t, statement)
	require.Equal(t, SearchResultSolved, result.Reason())
	require.Greater(t, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels), before)
}

func TestSearchMaxSolvableKStopsAfterTerminalPartialProbe(t *testing.T) {
	probes := map[int]*SearchResult{
		1: solvedSearchResult(&solutionResult{solved: true}, false),
		2: terminalSearchResult(SearchResultDeadlineExhausted, false),
	}

	maxSolvedK, result := searchMaxSolvableK(3, func(k int) *SearchResult {
		return probes[k]
	})

	require.Equal(t, 0, maxSolvedK)
	require.Equal(t, SearchResultDeadlineExhausted, result.Reason())
}

func jobSolverResultTestFeasibleNodes(ssn *framework.Session) []*node_info.NodeInfo {
	nodes := make([]*node_info.NodeInfo, 0, len(ssn.ClusterInfo.Nodes))
	for _, node := range ssn.ClusterInfo.Nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

func unrecordedVictimsForProbe(
	victimTasks []*pod_info.PodInfo, recordedVictims []*pod_info.PodInfo, probeK int,
) []*pod_info.PodInfo {
	recordedByUID := map[common_info.PodID]struct{}{}
	for _, task := range recordedVictims {
		recordedByUID[task.UID] = struct{}{}
	}

	neededVictims := probeK - len(recordedVictims)
	if neededVictims <= 0 {
		return nil
	}

	selectedVictims := make([]*pod_info.PodInfo, 0, neededVictims)
	for _, task := range victimTasks {
		if _, alreadyRecorded := recordedByUID[task.UID]; alreadyRecorded {
			continue
		}
		selectedVictims = append(selectedVictims, task)
		if len(selectedVictims) == neededVictims {
			return selectedVictims
		}
	}
	return selectedVictims
}

func newJobSolverResultTestSession(t *testing.T, tasksCount int) (*framework.Session, *podgroup_info.PodGroupInfo) {
	t.Helper()

	pendingJob, _ := createJobWithTasks(tasksCount, 1, "team-a", v1.PodPending, nil)
	defaultQueue := createQueue("default")
	defaultQueue.ParentQueue = ""
	submitQueue := createQueue("team-a")

	return &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
				pendingJob.UID: pendingJob,
			},
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				defaultQueue.UID: defaultQueue,
				submitQueue.UID:  submitQueue,
			},
			Nodes: map[string]*node_info.NodeInfo{},
		},
	}, pendingJob
}

func scenarioSearchCounterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()

	metric := scenarioSearchMetric(t, metricName, labels)
	if metric == nil || metric.GetCounter() == nil {
		return 0
	}
	return metric.GetCounter().GetValue()
}

func scenarioSearchHistogramCount(t *testing.T, metricName string, labels map[string]string) uint64 {
	t.Helper()

	metric := scenarioSearchMetric(t, metricName, labels)
	if metric == nil || metric.GetHistogram() == nil {
		return 0
	}
	return metric.GetHistogram().GetSampleCount()
}

func scenarioSearchMetric(t *testing.T, metricName string, labels map[string]string) *dto.Metric {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			if scenarioSearchMetricHasLabels(metric, labels) {
				return metric
			}
		}
	}
	return nil
}

func scenarioSearchMetricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		expectedValue, found := labels[label.GetName()]
		if !found || expectedValue != label.GetValue() {
			return false
		}
	}
	return true
}
