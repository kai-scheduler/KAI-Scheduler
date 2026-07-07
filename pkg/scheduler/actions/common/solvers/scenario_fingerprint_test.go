// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"crypto/sha256"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestFingerprintScenarioIsOrderIndependent(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)

	allAtOnce := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)

	oneByOneReversed := scenario.NewByNodeScenario(ssn, pendingJob, reversedTasks(pendingTasks), nil, nil)
	for index := len(victimTasks) - 1; index >= 0; index-- {
		oneByOneReversed.AddPotentialVictimsTasks([]*pod_info.PodInfo{victimTasks[index]})
	}

	require.Equal(t, fingerprintScenario(allAtOnce), fingerprintScenario(oneByOneReversed))
}

func TestFingerprintScenarioEncodesCanonicalSections(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	recordedJob, _ := addGeneratorTestJob(t, ssn, 1, 30, "team-recorded", "node-3")

	sn := scenario.NewByNodeScenario(
		ssn, pendingJob, pendingTasks, victimTasks, []*podgroup_info.PodGroupInfo{recordedJob},
	)

	payload := "10" + // preemptor job UID
		"\x1f" + "20" + "\x00" + "21" + // pending task UIDs
		"\x1f" + "30" + // recorded victim task UIDs
		"\x1f" + "40" + "\x00" + "41" // potential victim task UIDs

	require.Equal(t, scenarioFingerprint(sha256.Sum256([]byte(payload))), fingerprintScenario(sn))
}

func TestFingerprintScenarioDistinguishesInputs(t *testing.T) {
	ssn, pendingJob, victimTasks := newDedupCacheTestSession(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	recordedJob, _ := addGeneratorTestJob(t, ssn, 1, 30, "team-recorded", "node-3")

	base := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)

	differentVictims := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks[:1], nil)
	differentPending := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks[:1], victimTasks, nil)
	differentRecorded := scenario.NewByNodeScenario(
		ssn, pendingJob, pendingTasks, victimTasks, []*podgroup_info.PodGroupInfo{recordedJob},
	)

	baseFingerprint := fingerprintScenario(base)
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentVictims))
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentPending))
	require.NotEqual(t, baseFingerprint, fingerprintScenario(differentRecorded))
}

func TestSolvePartialJobSkipsRecordedDuplicates(t *testing.T) {
	ssn, solver, pendingJob, victimTasks := newSolverDedupTestSetup(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	failing := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, nil, nil)
	failingEquivalent := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, nil, nil)
	solving := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
	labels := map[string]string{"action": "reclaim", "generator": "dedup-skip", "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)

	result := solvePartialJobForDedupTest(t, solver, ssn, pendingJob, "dedup-skip",
		[]api.ScenarioInfo{failing, failingEquivalent, solving})

	require.Equal(t, SearchResultSolved, result.Reason())
	result.solution.statement.Discard()
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels))
	require.True(t, solver.failedScenarios.Has(fingerprintScenario(failing)))
}

func TestSolvePartialJobDoesNotRecordSolvedScenarios(t *testing.T) {
	ssn, solver, pendingJob, victimTasks := newSolverDedupTestSetup(t)
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	solving := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
	solvedFingerprint := fingerprintScenario(solving)

	result := solvePartialJobForDedupTest(t, solver, ssn, pendingJob, "dedup-solved",
		[]api.ScenarioInfo{solving})

	require.Equal(t, SearchResultSolved, result.Reason())
	result.solution.statement.Discard()
	require.False(t, solver.failedScenarios.Has(solvedFingerprint))
}

func TestSolvePartialJobNilCacheDisablesDedup(t *testing.T) {
	ssn, solver, pendingJob, _ := newSolverDedupTestSetup(t)
	solver.failedScenarios = nil
	pendingTasks := dedupCacheTestPendingTasks(ssn, pendingJob)
	failing := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, nil, nil)
	failingEquivalent := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, nil, nil)
	simulatedLabels := map[string]string{"action": "reclaim", "generator": "nil-cache", "state": "simulated"}
	duplicateLabels := map[string]string{"action": "reclaim", "generator": "nil-cache", "state": scenarioStateDuplicate}
	simulatedBefore := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", simulatedLabels)
	duplicateBefore := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", duplicateLabels)

	result := solvePartialJobForDedupTest(t, solver, ssn, pendingJob, "nil-cache",
		[]api.ScenarioInfo{failing, failingEquivalent})

	require.NotEqual(t, SearchResultSolved, result.Reason())
	require.Equal(t, simulatedBefore+2, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", simulatedLabels))
	require.Equal(t, duplicateBefore, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", duplicateLabels))
}

func TestSolveWithResultDedupsAcrossGenerators(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	require.NoError(t, ssn.InitNodeScoringPool())
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")

	ssn.AddScenarioGenerator("cross-first", func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		pendingTasks := dedupCacheTestPendingTasks(ssn, solveCtx.PartialPendingJob)
		failing := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks, nil, solveCtx.RecordedVictimsJobs,
		)
		return &portfolioTestGenerator{name: "cross-first", scenarios: []api.ScenarioInfo{failing}}
	})
	ssn.AddScenarioGenerator("cross-second", func(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		solveCtx := ctx.(*SolveContext)
		pendingTasks := dedupCacheTestPendingTasks(ssn, solveCtx.PartialPendingJob)
		failingEquivalent := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks, nil, solveCtx.RecordedVictimsJobs,
		)
		solving := scenario.NewByNodeScenario(
			ssn, solveCtx.PartialPendingJob, pendingTasks,
			unrecordedVictimsForProbe(victimTasks, solveCtx.RecordedVictimsTasks, solveCtx.ProbeK),
			solveCtx.RecordedVictimsJobs,
		)
		return &portfolioTestGenerator{name: "cross-second", scenarios: []api.ScenarioInfo{failingEquivalent, solving}}
	})
	solver := NewJobsSolver(
		jobSolverResultTestFeasibleNodes(ssn), nil, generatorTestVictimsQueueFactory(ssn, victimJob),
		framework.Reclaim, nil,
	)
	labels := map[string]string{"action": "reclaim", "generator": "cross-second", "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)

	solved, statement, _, result := solver.SolveWithResult(ssn, pendingJob)
	if statement != nil {
		defer statement.Discard()
	}

	require.True(t, solved)
	require.Equal(t, SearchResultSolved, result.Reason())
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels))
}

func newSolverDedupTestSetup(t *testing.T) (*framework.Session, *JobSolver, *podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	require.NoError(t, ssn.InitNodeScoringPool())
	_, victimTasks := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	solver := NewJobsSolver(
		jobSolverResultTestFeasibleNodes(ssn), nil, generatorTestVictimsQueueFactory(ssn), framework.Reclaim, nil,
	)
	solver.failedScenarios = sets.New[scenarioFingerprint]()
	return ssn, solver, pendingJob, victimTasks
}

func solvePartialJobForDedupTest(
	t *testing.T, solver *JobSolver, ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo,
	generatorName string, scenarios []api.ScenarioInfo,
) *SearchResult {
	t.Helper()

	registration := framework.ScenarioGeneratorRegistration{
		Name:    generatorName,
		Factory: portfolioTestFactory(&portfolioTestGenerator{name: generatorName, scenarios: scenarios}),
	}
	return solver.solvePartialJob(
		ssn, &solvingState{}, pendingJob,
		newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(),
		registration, nil, 1,
	)
}

func newDedupCacheTestSession(t *testing.T) (*framework.Session, *podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1, "node-2": 1, "node-3": 1})
	pendingJob := addGeneratorTestPendingJob(t, ssn, 2, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 2)
	_, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-1", "node-2")
	return ssn, pendingJob, victimTasks
}

func dedupCacheTestPendingTasks(ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	return podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
}

func reversedTasks(tasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	reversed := slices.Clone(tasks)
	slices.Reverse(reversed)
	return reversed
}
