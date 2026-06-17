// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}

func TestActionSearchBudgetDefaults(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	budget, err := newActionSearchBudgetWithClock(&framework.Session{}, framework.Reclaim, clock.Now)

	require.NoError(t, err)
	require.Equal(t, framework.Reclaim, budget.action)
	require.Equal(t, 20*time.Second, budget.actionLimit)
	require.Equal(t, 10*time.Second, budget.jobLimit)
	require.Equal(t, time.Duration(0), budget.minJobSearch)
	require.Equal(t, 5*time.Second, budget.generatorLimits[scenariosearch.GeneratorNodeLocalGreedy])
	require.Equal(t, 5*time.Second, budget.generatorLimits[scenariosearch.GeneratorMultiNodeGang])
	require.Equal(t, 5*time.Second, budget.generatorLimits[scenariosearch.ActionDefault])
}

func TestActionSearchBudgetParsesDurationStrings(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ssn := sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]string{
			scenariosearch.ActionDefault: "30s",
			scenariosearch.ActionPreempt: "3s",
		},
		MaxJobSearchDuration: "750ms",
		MinJobSearchDuration: "100ms",
		MaxGeneratorSearchDuration: map[string]string{
			scenariosearch.ActionDefault:            "400ms",
			scenariosearch.GeneratorNodeLocalGreedy: "75ms",
		},
	})

	budget, err := newActionSearchBudgetWithClock(ssn, framework.Preempt, clock.Now)

	require.NoError(t, err)
	require.Equal(t, 3*time.Second, budget.actionLimit)
	require.Equal(t, 750*time.Millisecond, budget.jobLimit)
	require.Equal(t, 100*time.Millisecond, budget.minJobSearch)
	require.Equal(t, 75*time.Millisecond, budget.generatorLimits[scenariosearch.GeneratorNodeLocalGreedy])
	require.Equal(t, 400*time.Millisecond, budget.generatorLimits[scenariosearch.GeneratorMultiNodeGang])
	require.Equal(t, 400*time.Millisecond, budget.generatorLimits[scenariosearch.ActionDefault])
}

func TestActionSearchBudgetRejectsNegativeDurations(t *testing.T) {
	tests := []struct {
		name    string
		budgets *conf.ScenarioSearchBudgets
	}{
		{
			name: "action",
			budgets: &conf.ScenarioSearchBudgets{
				MaxActionSearchDuration: map[string]string{
					scenariosearch.ActionReclaim: "-1s",
				},
			},
		},
		{
			name: "job",
			budgets: &conf.ScenarioSearchBudgets{
				MaxJobSearchDuration: "-1s",
			},
		},
		{
			name: "min job",
			budgets: &conf.ScenarioSearchBudgets{
				MinJobSearchDuration: "-1s",
			},
		},
		{
			name: "generator",
			budgets: &conf.ScenarioSearchBudgets{
				MaxGeneratorSearchDuration: map[string]string{
					scenariosearch.GeneratorNodeLocalGreedy: "-1s",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newActionSearchBudgetWithClock(
				sessionWithScenarioSearchBudgets(test.budgets),
				framework.Reclaim,
				time.Now,
			)

			require.Error(t, err)
		})
	}
}

func TestActionSearchBudgetRejectsInvalidDurations(t *testing.T) {
	tests := []struct {
		name          string
		budgets       *conf.ScenarioSearchBudgets
		errorContains string
	}{
		{
			name: "action",
			budgets: &conf.ScenarioSearchBudgets{
				MaxActionSearchDuration: map[string]string{
					scenariosearch.ActionReclaim: "invalid",
				},
			},
			errorContains: `maxActionSearchDuration["reclaim"]`,
		},
		{
			name: "job",
			budgets: &conf.ScenarioSearchBudgets{
				MaxJobSearchDuration: "invalid",
			},
			errorContains: "maxJobSearchDuration",
		},
		{
			name: "min job",
			budgets: &conf.ScenarioSearchBudgets{
				MinJobSearchDuration: "invalid",
			},
			errorContains: "minJobSearchDuration",
		},
		{
			name: "generator",
			budgets: &conf.ScenarioSearchBudgets{
				MaxGeneratorSearchDuration: map[string]string{
					scenariosearch.GeneratorNodeLocalGreedy: "invalid",
				},
			},
			errorContains: `maxGeneratorSearchDuration["NodeLocalGreedy"]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newActionSearchBudgetWithClock(
				sessionWithScenarioSearchBudgets(test.budgets),
				framework.Reclaim,
				time.Now,
			)

			require.ErrorContains(t, err, test.errorContains)
		})
	}
}

func TestActionSearchBudgetRejectsMinJobAtOrAboveMaxJob(t *testing.T) {
	tests := []struct {
		name string
		min  string
		max  string
	}{
		{
			name: "equal",
			min:  "250ms",
			max:  "250ms",
		},
		{
			name: "above",
			min:  "251ms",
			max:  "250ms",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newActionSearchBudgetWithClock(
				sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
					MaxJobSearchDuration: test.max,
					MinJobSearchDuration: test.min,
				}),
				framework.Reclaim,
				time.Now,
			)

			require.Error(t, err)
		})
	}
}

func TestBeginJobMarksReducedBudgetWhenRemainingBelowMin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "100ms",
			},
			MaxJobSearchDuration: "1s",
			MinJobSearchDuration: "50ms",
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(75 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.True(t, jobBudget.ReducedBudget())
	require.Equal(t, 25*time.Millisecond, jobBudget.Remaining())
}

func TestBeginJobReturnsNotAttemptedWhenActionBudgetExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "100ms",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(100 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.False(t, jobBudget.ReducedBudget())
	require.True(t, budget.Exhausted())
	require.Equal(t, time.Duration(0), jobBudget.Remaining())
}

func TestNilActionSearchBudgetBeginJobAndGeneratorAreSafe(t *testing.T) {
	var budget *ActionSearchBudget

	var jobBudget *jobSearchBudget
	var generatorBudget *generatorSearchBudget
	require.NotPanics(t, func() {
		jobBudget = budget.BeginJob()
		generatorBudget = jobBudget.BeginGenerator("x")
	})

	require.False(t, jobBudget.ReducedBudget())
	require.Equal(t, time.Duration(0), jobBudget.Remaining())
	require.True(t, generatorBudget.Exhausted())
	require.Equal(t, time.Duration(0), generatorBudget.Remaining())
}

func TestUnlimitedActionBudgetUsesJobLimit(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "0",
			},
			MaxJobSearchDuration: "100ms",
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	require.False(t, budget.Exhausted())
	require.Equal(t, unlimitedRemaining, budget.Remaining())

	jobBudget := budget.BeginJob()
	require.False(t, jobBudget.ReducedBudget())
	require.Equal(t, 100*time.Millisecond, jobBudget.Remaining())
}

func TestUnlimitedJobBudgetUsesActionRemaining(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "500ms",
			},
			MaxJobSearchDuration: "0",
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(100 * time.Millisecond)
	jobBudget := budget.BeginJob()
	require.Equal(t, 400*time.Millisecond, jobBudget.Remaining())

	clock.Advance(250 * time.Millisecond)
	require.Equal(t, 150*time.Millisecond, jobBudget.Remaining())
}

func TestGeneratorBudgetUsesSpecificThenDefaultLimit(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "1s",
			},
			MaxJobSearchDuration: "1s",
			MaxGeneratorSearchDuration: map[string]string{
				scenariosearch.ActionDefault:            "400ms",
				scenariosearch.GeneratorNodeLocalGreedy: "75ms",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	jobBudget := budget.BeginJob()
	require.Equal(t, 75*time.Millisecond, jobBudget.BeginGenerator(scenariosearch.GeneratorNodeLocalGreedy).Remaining())
	require.Equal(t, 400*time.Millisecond, jobBudget.BeginGenerator("unknown").Remaining())
	require.Equal(t, 400*time.Millisecond, jobBudget.BeginGenerator(scenariosearch.GeneratorMultiNodeGang).Remaining())
}

func TestGeneratorBudgetMovesToNextGeneratorWhenExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&conf.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]string{
				scenariosearch.ActionReclaim: "1s",
			},
			MaxJobSearchDuration: "1s",
			MaxGeneratorSearchDuration: map[string]string{
				scenariosearch.ActionDefault:            "200ms",
				scenariosearch.GeneratorNodeLocalGreedy: "50ms",
				scenariosearch.GeneratorMultiNodeGang:   "0",
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	jobBudget := budget.BeginJob()
	firstGeneratorBudget := jobBudget.BeginGenerator(scenariosearch.GeneratorNodeLocalGreedy)
	clock.Advance(60 * time.Millisecond)

	require.True(t, firstGeneratorBudget.Exhausted())
	require.Equal(t, time.Duration(0), firstGeneratorBudget.Remaining())

	secondGeneratorBudget := jobBudget.BeginGenerator(scenariosearch.GeneratorMultiNodeGang)
	clock.Advance(500 * time.Millisecond)

	require.False(t, secondGeneratorBudget.Exhausted())
	require.Equal(t, 440*time.Millisecond, secondGeneratorBudget.Remaining())
}

func TestSearchResultNilReceiver(t *testing.T) {
	var result *SearchResult

	require.Empty(t, result.Reason())
	require.False(t, result.ReducedBudget())
	require.False(t, result.EnteredSearch())
}

func sessionWithScenarioSearchBudgets(budgets *conf.ScenarioSearchBudgets) *framework.Session {
	return &framework.Session{
		Config: &conf.SchedulerConfiguration{
			ScenarioSearchBudgets: budgets,
		},
	}
}
