// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestScenarioPortfolioUsesRegistrationOrder(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	secondScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	firstGenerator := &portfolioTestGenerator{name: "first", scenarios: []api.ScenarioInfo{firstScenario}}
	secondGenerator := &portfolioTestGenerator{name: "second", scenarios: []api.ScenarioInfo{secondScenario}}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), nil)

	require.Same(t, firstScenario, portfolio.Next())
	require.Same(t, secondScenario, portfolio.Next())
	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultGeneratorsExhausted, portfolio.StopReason())
}

func TestScenarioPortfolioDoesNotChargeGeneratorBuildTimeToGeneratorDeadline(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	secondScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	firstGenerator := &portfolioTestGenerator{
		name:      constants.GeneratorNodeLocalGreedy,
		scenarios: []api.ScenarioInfo{firstScenario},
		onNext: func() {
			clock.Advance(2 * time.Millisecond)
		},
	}
	secondGenerator := &portfolioTestGenerator{
		name:      constants.GeneratorMultiNodeGang,
		scenarios: []api.ScenarioInfo{secondScenario},
	}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("1s"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MaxGeneratorSearchDuration: map[string]metav1.Duration{
				constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("1ms"),
				constants.GeneratorMultiNodeGang:   scenarioSearchDurationForTest("1s"),
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	portfolio := newScenarioPortfolio(ctx, actionBudget.BeginJob(), nil)

	require.Same(t, firstScenario, portfolio.Next())
	require.Same(t, secondScenario, portfolio.Next())
	require.Equal(t, 1, firstGenerator.nextCalls)
	require.Equal(t, 1, secondGenerator.nextCalls)
}

func TestScenarioPortfolioDoesNotChargeGeneratorBuildTimeToJobDeadline(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	generator := &portfolioTestGenerator{
		name:      constants.GeneratorNodeLocalGreedy,
		scenarios: []api.ScenarioInfo{firstScenario},
		onNext: func() {
			clock.Advance(2 * time.Millisecond)
		},
	}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(generator))
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("1s"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1ms"),
			MaxGeneratorSearchDuration: map[string]metav1.Duration{
				constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("1s"),
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	portfolio := newScenarioPortfolio(ctx, actionBudget.BeginJob(), nil)

	require.Same(t, firstScenario, portfolio.Next())
}

func TestScenarioPortfolioReturnsNoGeneratorWhenNoAvailableGenerators(t *testing.T) {
	ctx, _, _ := newScenarioPortfolioTestContext(t, framework.Reclaim)
	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), nil)

	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultNoGenerator, portfolio.StopReason())
}

func TestScenarioPortfolioReturnsGeneratorsExhaustedAfterAllGeneratorsEnd(t *testing.T) {
	ctx, _, _ := newScenarioPortfolioTestContext(t, framework.Reclaim)
	firstGenerator := &portfolioTestGenerator{name: "first"}
	secondGenerator := &portfolioTestGenerator{name: "second"}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), nil)

	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultGeneratorsExhausted, portfolio.StopReason())
	require.Equal(t, 1, firstGenerator.nextCalls)
	require.Equal(t, 1, secondGenerator.nextCalls)
}

func TestScenarioPortfolioSkipsNonByNodeScenarios(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	firstGenerator := &portfolioTestGenerator{
		name:      "first",
		scenarios: []api.ScenarioInfo{portfolioTestScenarioInfo{}},
	}
	secondGenerator := &portfolioTestGenerator{name: "second", scenarios: []api.ScenarioInfo{firstScenario}}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), nil)

	require.Same(t, firstScenario, portfolio.Next())
	require.Equal(t, 1, firstGenerator.nextCalls)
	require.Equal(t, 1, secondGenerator.nextCalls)
}

func TestScenarioPortfolioTreatsTypedNilScenarioAsGeneratorExhaustion(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	var typedNil *scenario.ByNodeScenario
	firstGenerator := &portfolioTestGenerator{name: "first", scenarios: []api.ScenarioInfo{typedNil}}
	secondGenerator := &portfolioTestGenerator{name: "second", scenarios: []api.ScenarioInfo{firstScenario}}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), newScenarioDedupCache(),
	)

	require.Same(t, firstScenario, portfolio.Next())
	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultGeneratorsExhausted, portfolio.StopReason())
}

func TestScenarioPortfolioSkipsRecordedDuplicateScenarios(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstEquivalent := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	second := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks[:1], nil)
	generatorName := "dedup-skip"
	ctx.Session.AddScenarioGenerator(generatorName, portfolioTestFactory(&portfolioTestGenerator{
		name:      generatorName,
		scenarios: []api.ScenarioInfo{first, firstEquivalent, second},
	}))
	labels := map[string]string{"action": "reclaim", "generator": generatorName, "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)

	portfolio := newScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), newScenarioDedupCache(),
	)

	require.Same(t, first, portfolio.Next())
	portfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)
	require.Same(t, second, portfolio.Next())
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels))
}

func TestScenarioPortfolioDoesNotSkipDuplicatesOfSolvedScenarios(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstEquivalent := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	generatorName := "dedup-solved"
	ctx.Session.AddScenarioGenerator(generatorName, portfolioTestFactory(&portfolioTestGenerator{
		name:      generatorName,
		scenarios: []api.ScenarioInfo{first, firstEquivalent},
	}))
	labels := map[string]string{"action": "reclaim", "generator": generatorName, "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)

	portfolio := newScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), newScenarioDedupCache(),
	)

	// A solved scenario must remain re-emittable: the final probe rebuilds the
	// winning statement by re-running the generator.
	require.Same(t, first, portfolio.Next())
	portfolio.ObserveCurrentAttempt(string(SearchResultSolved))
	require.Same(t, firstEquivalent, portfolio.Next())
	require.Equal(t, before, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels))
}

func TestScenarioPortfolioDoesNotDedupeDifferentSimulationContexts(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	recordedJob, _ := addGeneratorTestJob(t, ctx.Session, 1, 30, "team-recorded", "node-1")
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	differentPending := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks[:1], victimTasks, nil)
	differentRecorded := scenario.NewByNodeScenario(
		ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, []*podgroup_info.PodGroupInfo{recordedJob},
	)
	ctx.Session.AddScenarioGenerator("dedup-context", portfolioTestFactory(&portfolioTestGenerator{
		name:      "dedup-context",
		scenarios: []api.ScenarioInfo{first, differentPending, differentRecorded},
	}))

	portfolio := newScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), newScenarioDedupCache(),
	)

	require.Same(t, first, portfolio.Next())
	portfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)
	require.Same(t, differentPending, portfolio.Next())
	portfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)
	require.Same(t, differentRecorded, portfolio.Next())
}

func TestScenarioPortfolioDedupsAcrossGenerators(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstEquivalent := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	second := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks[:1], nil)
	ctx.Session.AddScenarioGenerator("cross-first", portfolioTestFactory(&portfolioTestGenerator{
		name: "cross-first", scenarios: []api.ScenarioInfo{first},
	}))
	ctx.Session.AddScenarioGenerator("cross-second", portfolioTestFactory(&portfolioTestGenerator{
		name: "cross-second", scenarios: []api.ScenarioInfo{firstEquivalent, second},
	}))
	labels := map[string]string{"action": "reclaim", "generator": "cross-second", "state": scenarioStateDuplicate}
	before := scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels)

	portfolio := newScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), newScenarioDedupCache(),
	)

	require.Same(t, first, portfolio.Next())
	portfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)
	require.Same(t, second, portfolio.Next())
	require.Equal(t, before+1, scenarioSearchCounterValue(t, "scenario_search_scenarios_total", labels))
}

func TestScenarioPortfolioSharedCacheDedupsAcrossPortfolios(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstEquivalent := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstRegistration := framework.ScenarioGeneratorRegistration{
		Name: "portfolio-first",
		Factory: portfolioTestFactory(&portfolioTestGenerator{
			name: "portfolio-first", scenarios: []api.ScenarioInfo{first},
		}),
	}
	secondRegistration := framework.ScenarioGeneratorRegistration{
		Name: "portfolio-second",
		Factory: portfolioTestFactory(&portfolioTestGenerator{
			name: "portfolio-second", scenarios: []api.ScenarioInfo{firstEquivalent},
		}),
	}
	sharedCache := newScenarioDedupCache()

	firstPortfolio := newSingleGeneratorScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), firstRegistration, nil, sharedCache,
	)
	require.Same(t, first, firstPortfolio.Next())
	firstPortfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)

	secondPortfolio := newSingleGeneratorScenarioPortfolio(
		ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), secondRegistration, nil, sharedCache,
	)
	require.Nil(t, secondPortfolio.Next())
	require.Equal(t, SearchResultGeneratorsExhausted, secondPortfolio.StopReason())
}

func TestScenarioPortfolioNilCacheDisablesDedup(t *testing.T) {
	ctx, victimTasks, pendingTasks := newPortfolioDedupTestContext(t)
	first := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	firstEquivalent := scenario.NewByNodeScenario(ctx.Session, ctx.PartialPendingJob, pendingTasks, victimTasks, nil)
	ctx.Session.AddScenarioGenerator("nil-cache", portfolioTestFactory(&portfolioTestGenerator{
		name: "nil-cache", scenarios: []api.ScenarioInfo{first, firstEquivalent},
	}))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob(), nil)

	require.Same(t, first, portfolio.Next())
	portfolio.ObserveCurrentAttempt(scenarioSearchResultUnsolved)
	require.Same(t, firstEquivalent, portfolio.Next())
}

func newPortfolioDedupTestContext(t *testing.T) (*SolveContext, []*pod_info.PodInfo, []*pod_info.PodInfo) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	pendingJob := addGeneratorTestPendingJob(t, ssn, 2, 10, "team-pending")
	setGeneratorTestMinAvailable(pendingJob, 2)
	_, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-1")
	ctx := &SolveContext{
		Session:              ssn,
		ActionType:           framework.Reclaim,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	}
	return ctx, victimTasks, dedupCacheTestPendingTasks(ssn, pendingJob)
}

func newScenarioPortfolioTestContext(
	t *testing.T, action framework.ActionType,
) (*SolveContext, *podgroup_info.PodGroupInfo, *scenario.ByNodeScenario) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	sn := newPortfolioTestByNodeScenario(t, ssn, pendingJob)
	ctx := &SolveContext{
		Session:              ssn,
		ActionType:           action,
		PartialPendingJob:    pendingJob,
		GenerateVictimsQueue: generatorTestVictimsQueueFactory(ssn),
		FeasibleNodes:        ssn.ClusterInfo.Nodes,
	}
	return ctx, pendingJob, sn
}

func newPortfolioTestByNodeScenario(
	t *testing.T, ssn *framework.Session, pendingJob *podgroup_info.PodGroupInfo,
) *scenario.ByNodeScenario {
	t.Helper()

	pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	return scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, nil, nil)
}

func portfolioTestFactory(generator framework.ScenarioGenerator) framework.ScenarioGeneratorFactory {
	return func(framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
		return generator
	}
}

type portfolioTestGenerator struct {
	name      string
	scenarios []api.ScenarioInfo
	onNext    func()
	nextCalls int
}

func (g *portfolioTestGenerator) Name() string {
	return g.name
}

func (g *portfolioTestGenerator) Next() api.ScenarioInfo {
	g.nextCalls++
	if g.onNext != nil {
		g.onNext()
	}
	if len(g.scenarios) == 0 {
		return nil
	}
	sn := g.scenarios[0]
	g.scenarios = g.scenarios[1:]
	return sn
}

type portfolioTestScenarioInfo struct{}

func (portfolioTestScenarioInfo) GetPreemptor() *podgroup_info.PodGroupInfo {
	return nil
}

func (portfolioTestScenarioInfo) GetVictims() map[common_info.PodGroupID]*api.VictimInfo {
	return nil
}
