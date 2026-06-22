// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestScenarioPortfolioUsesRegistrationOrder(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	secondScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	firstGenerator := &portfolioTestGenerator{name: "first", scenarios: []api.ScenarioInfo{firstScenario}}
	secondGenerator := &portfolioTestGenerator{name: "second", scenarios: []api.ScenarioInfo{secondScenario}}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob())

	require.Same(t, firstScenario, portfolio.Next())
	require.Same(t, secondScenario, portfolio.Next())
	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultGeneratorsExhausted, portfolio.StopReason())
}

func TestScenarioPortfolioFiltersByAction(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	preemptOnly := &portfolioTestGenerator{name: "preempt", scenarios: []api.ScenarioInfo{
		newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob),
	}}
	reclaimOnly := &portfolioTestGenerator{name: "reclaim", scenarios: []api.ScenarioInfo{firstScenario}}
	allActionsScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	allActions := &portfolioTestGenerator{name: "all", scenarios: []api.ScenarioInfo{allActionsScenario}}
	ctx.Session.AddScenarioGenerator("preempt", portfolioTestFactory(preemptOnly), framework.Preempt)
	ctx.Session.AddScenarioGenerator("reclaim", portfolioTestFactory(reclaimOnly), framework.Reclaim)
	ctx.Session.AddScenarioGenerator("all", portfolioTestFactory(allActions))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob())

	require.Same(t, firstScenario, portfolio.Next())
	require.Same(t, allActionsScenario, portfolio.Next())
	require.Nil(t, portfolio.Next())
	require.Zero(t, preemptOnly.nextCalls)
	require.Equal(t, SearchResultGeneratorsExhausted, portfolio.StopReason())
}

func TestScenarioPortfolioMovesToNextGeneratorAfterGeneratorDeadline(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	secondScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	firstGenerator := &portfolioTestGenerator{
		name:      scenariosearch.GeneratorNodeLocalGreedy,
		scenarios: []api.ScenarioInfo{firstScenario},
		onNext: func() {
			clock.Advance(2 * time.Millisecond)
		},
	}
	secondGenerator := &portfolioTestGenerator{
		name:      scenariosearch.GeneratorMultiNodeGang,
		scenarios: []api.ScenarioInfo{secondScenario},
	}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{scenariosearch.ActionReclaim: "1s"},
			MaxJobSearchDuration:    "1s",
			MaxGeneratorSearchDuration: map[string]string{
				scenariosearch.GeneratorNodeLocalGreedy: "1ms",
				scenariosearch.GeneratorMultiNodeGang:   "1s",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	portfolio := newScenarioPortfolio(ctx, actionBudget.BeginJob())

	require.Same(t, secondScenario, portfolio.Next())
	require.Equal(t, 1, firstGenerator.nextCalls)
	require.Equal(t, 1, secondGenerator.nextCalls)
}

func TestScenarioPortfolioRecordsGeneratorBudgetExhaustedDuration(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	secondScenario := newPortfolioTestByNodeScenario(t, ctx.Session, ctx.PartialPendingJob)
	firstGeneratorName := "test-generator-budget-exhausted"
	secondGeneratorName := "test-generator-budget-next"
	firstGenerator := &portfolioTestGenerator{
		name:      firstGeneratorName,
		scenarios: []api.ScenarioInfo{firstScenario},
		onNext: func() {
			clock.Advance(2 * time.Millisecond)
		},
	}
	secondGenerator := &portfolioTestGenerator{
		name:      secondGeneratorName,
		scenarios: []api.ScenarioInfo{secondScenario},
	}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{scenariosearch.ActionReclaim: "1s"},
			MaxJobSearchDuration:    "1s",
			MaxGeneratorSearchDuration: map[string]string{
				firstGeneratorName:  "1ms",
				secondGeneratorName: "1s",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)
	labels := map[string]string{
		"action":    "reclaim",
		"generator": firstGeneratorName,
		"result":    scenarioSearchResultGeneratorBudgetExhausted,
	}
	before := scenarioSearchHistogramCount(t, "scenario_search_duration_seconds", labels)

	portfolio := newScenarioPortfolio(ctx, actionBudget.BeginJob())

	require.Same(t, secondScenario, portfolio.Next())
	require.Equal(t, before+1, scenarioSearchHistogramCount(t, "scenario_search_duration_seconds", labels))
}

func TestScenarioPortfolioReturnsDeadlineExhaustedAfterJobDeadline(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	generator := &portfolioTestGenerator{
		name:      scenariosearch.GeneratorNodeLocalGreedy,
		scenarios: []api.ScenarioInfo{firstScenario},
		onNext: func() {
			clock.Advance(2 * time.Millisecond)
		},
	}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(generator))
	actionBudget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{scenariosearch.ActionReclaim: "1s"},
			MaxJobSearchDuration:    "1ms",
			MaxGeneratorSearchDuration: map[string]string{
				scenariosearch.GeneratorNodeLocalGreedy: "1s",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	portfolio := newScenarioPortfolio(ctx, actionBudget.BeginJob())

	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultDeadlineExhausted, portfolio.StopReason())
	require.False(t, portfolio.enteredSearch)
}

func TestScenarioPortfolioReturnsNoGeneratorWhenNoRegistrationApplies(t *testing.T) {
	ctx, _, firstScenario := newScenarioPortfolioTestContext(t, framework.Reclaim)
	preemptOnly := &portfolioTestGenerator{name: "preempt", scenarios: []api.ScenarioInfo{firstScenario}}
	ctx.Session.AddScenarioGenerator("preempt", portfolioTestFactory(preemptOnly), framework.Preempt)

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob())

	require.Nil(t, portfolio.Next())
	require.Equal(t, SearchResultNoGenerator, portfolio.StopReason())
	require.Zero(t, preemptOnly.nextCalls)
}

func TestScenarioPortfolioReturnsGeneratorsExhaustedAfterAllGeneratorsEnd(t *testing.T) {
	ctx, _, _ := newScenarioPortfolioTestContext(t, framework.Reclaim)
	firstGenerator := &portfolioTestGenerator{name: "first"}
	secondGenerator := &portfolioTestGenerator{name: "second"}
	ctx.Session.AddScenarioGenerator("first", portfolioTestFactory(firstGenerator))
	ctx.Session.AddScenarioGenerator("second", portfolioTestFactory(secondGenerator))

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob())

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

	portfolio := newScenarioPortfolio(ctx, newUnlimitedActionSearchBudget(framework.Reclaim).BeginJob())

	require.Same(t, firstScenario, portfolio.Next())
	require.Equal(t, 1, firstGenerator.nextCalls)
	require.Equal(t, 1, secondGenerator.nextCalls)
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
