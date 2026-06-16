// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestNewJobsSolverDefaultsNilBudgetToUnlimited(t *testing.T) {
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil)

	require.NotNil(t, solver.actionBudget)
	require.False(t, solver.actionBudget.Exhausted())
	require.Greater(t, solver.actionBudget.BeginJob().Remaining(), time.Hour)
}

func TestNewJobsSolverDefaultsOmittedBudgetToUnlimited(t *testing.T) {
	solver := NewJobsSolver(nil, nil, nil, framework.Reclaim)

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
	require.False(t, result.EnteredSearch())
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
	require.False(t, result.EnteredSearch())
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
	require.False(t, result.EnteredSearch())
}

func TestSearchMaxSolvableKSkipsSingleTaskFullProbe(t *testing.T) {
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	actionBudget := newUnlimitedActionSearchBudget(framework.Reclaim)
	jobBudget := actionBudget.BeginJob()
	solver := NewJobsSolver(
		nil,
		nil,
		func() *utils.JobsOrderByQueues {
			t.Fatal("single-task search must leave the full probe to SolveWithResult")
			return nil
		},
		framework.Reclaim,
		actionBudget,
	)
	tasksToAllocate := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)

	maxSolvedK, result := solver.searchMaxSolvableK(
		ssn, &solvingState{}, pendingJob, tasksToAllocate, jobBudget,
		framework.ScenarioGeneratorRegistration{}, nil,
	)

	require.Equal(t, 0, maxSolvedK)
	require.Nil(t, result)
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
	}, framework.Reclaim)
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
	}, framework.Reclaim)
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
	require.Equal(t, []string{"first:1", "second:1", "second:2", "second:3"}, factoryCalls)
}

func TestSolveWithResultReportsDeadlineBeforeScenarioSimulation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "10ms",
			},
			MaxJobSearchDuration: "1ms",
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)
	ssn, pendingJob := newJobSolverResultTestSession(t, 1)
	ssn.AddScenarioGenerator("deadline-test", NewMultiNodeGangGenerator, framework.Reclaim)
	solver := NewJobsSolver(
		nil,
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
	require.False(t, result.EnteredSearch())
}

func TestSearchMaxSolvableKPreservesEnteredSearchAfterTerminalPartialProbe(t *testing.T) {
	probes := map[int]*SearchResult{
		1: solvedSearchResult(&solutionResult{solved: true}, false),
		2: terminalSearchResult(SearchResultDeadlineExhausted, false, false),
	}

	maxSolvedK, result := searchMaxSolvableK(3, func(k int) *SearchResult {
		return probes[k]
	})

	require.Equal(t, 0, maxSolvedK)
	require.Equal(t, SearchResultDeadlineExhausted, result.Reason())
	require.True(t, result.EnteredSearch())
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

func TestPreserveEnteredSearchMarksTerminalResult(t *testing.T) {
	result := terminalSearchResult(SearchResultDeadlineExhausted, false, false)

	preserveEnteredSearch(result, true)

	require.True(t, result.EnteredSearch())
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
