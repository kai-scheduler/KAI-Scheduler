// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
	require.Equal(t, 5*time.Minute, budget.actionLimit)
	require.Equal(t, 4*time.Minute, budget.jobLimit)
	require.Equal(t, time.Duration(0), budget.minJobSearch)
	require.Equal(t, 30*time.Second, budget.generatorLimits[constants.GeneratorNodeLocalGreedy])
	require.Equal(t, 2*time.Minute, budget.generatorLimits[constants.GeneratorMultiNodeGang])
	require.Equal(t, 2*time.Minute, budget.generatorLimits[constants.ActionDefault])
}

func TestActionSearchBudgetUsesConfiguredDurations(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ssn := sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{
			constants.ActionDefault: scenarioSearchDurationForTest("30s"),
			constants.ActionPreempt: scenarioSearchDurationForTest("3s"),
		},
		MaxJobSearchDuration: scenarioSearchDurationPtrForTest("750ms"),
		MinJobSearchDuration: scenarioSearchDurationPtrForTest("100ms"),
		MaxGeneratorSearchDuration: map[string]metav1.Duration{
			constants.ActionDefault:            scenarioSearchDurationForTest("400ms"),
			constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("75ms"),
		},
	})

	budget, err := newActionSearchBudgetWithClock(ssn, framework.Preempt, clock.Now)

	require.NoError(t, err)
	require.Equal(t, 3*time.Second, budget.actionLimit)
	require.Equal(t, 750*time.Millisecond, budget.jobLimit)
	require.Equal(t, 100*time.Millisecond, budget.minJobSearch)
	require.Equal(t, 75*time.Millisecond, budget.generatorLimits[constants.GeneratorNodeLocalGreedy])
	require.Equal(t, 400*time.Millisecond, budget.generatorLimits[constants.GeneratorMultiNodeGang])
	require.Equal(t, 400*time.Millisecond, budget.generatorLimits[constants.ActionDefault])
}

func TestActionSearchBudgetRejectsNegativeDurations(t *testing.T) {
	tests := []struct {
		name    string
		budgets *kaiv1.ScenarioSearchBudgets
	}{
		{
			name: "action",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MaxActionSearchDuration: map[string]metav1.Duration{
					constants.ActionReclaim: scenarioSearchDurationForTest("-1s"),
				},
			},
		},
		{
			name: "job",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MaxJobSearchDuration: scenarioSearchDurationPtrForTest("-1s"),
			},
		},
		{
			name: "min job",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinJobSearchDuration: scenarioSearchDurationPtrForTest("-1s"),
			},
		},
		{
			name: "generator",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MaxGeneratorSearchDuration: map[string]metav1.Duration{
					constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("-1s"),
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
				sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
					MaxJobSearchDuration: scenarioSearchDurationPtrForTest(test.max),
					MinJobSearchDuration: scenarioSearchDurationPtrForTest(test.min),
				}),
				framework.Reclaim,
				time.Now,
			)

			require.Error(t, err)
		})
	}
}

func TestBeginJobGuaranteesMinSearchWhenRemainingBelowMin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("100ms"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MinJobSearchDuration: scenarioSearchDurationPtrForTest("50ms"),
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(75 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.True(t, jobBudget.ReducedBudget())
	require.Equal(t, 50*time.Millisecond, jobBudget.Remaining())
}

func TestBeginJobMarksReducedBudgetWhenRemainingBelowJobLimit(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("1s"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("500ms"),
			MinJobSearchDuration: scenarioSearchDurationPtrForTest("50ms"),
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(750 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.True(t, jobBudget.ReducedBudget())
	require.Equal(t, 250*time.Millisecond, jobBudget.Remaining())
}

func TestBeginJobGuaranteesMinSearchWhenActionBudgetExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("100ms"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MinJobSearchDuration: scenarioSearchDurationPtrForTest("50ms"),
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(100 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.True(t, budget.Exhausted())
	require.True(t, jobBudget.ReducedBudget())
	require.Equal(t, 50*time.Millisecond, jobBudget.Remaining())
}

func TestBeginJobReturnsNotAttemptedWhenActionBudgetExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("100ms"),
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	clock.Advance(100 * time.Millisecond)
	jobBudget := budget.BeginJob()

	require.True(t, jobBudget.ReducedBudget())
	require.True(t, budget.Exhausted())
	require.True(t, jobBudget.Exhausted())
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
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("0"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("100ms"),
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
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("500ms"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("0"),
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
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("1s"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MaxGeneratorSearchDuration: map[string]metav1.Duration{
				constants.ActionDefault:            scenarioSearchDurationForTest("400ms"),
				constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("75ms"),
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	jobBudget := budget.BeginJob()
	require.Equal(t, 75*time.Millisecond, jobBudget.BeginGenerator(constants.GeneratorNodeLocalGreedy).Remaining())
	require.Equal(t, 400*time.Millisecond, jobBudget.BeginGenerator("unknown").Remaining())
	require.Equal(t, 400*time.Millisecond, jobBudget.BeginGenerator(constants.GeneratorMultiNodeGang).Remaining())
}

func TestGeneratorBudgetMovesToNextGeneratorWhenExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	budget, err := newActionSearchBudgetWithClock(
		sessionWithScenarioSearchBudgets(&kaiv1.ScenarioSearchBudgets{
			MaxActionSearchDuration: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("1s"),
			},
			MaxJobSearchDuration: scenarioSearchDurationPtrForTest("1s"),
			MaxGeneratorSearchDuration: map[string]metav1.Duration{
				constants.ActionDefault:            scenarioSearchDurationForTest("200ms"),
				constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest("50ms"),
				constants.GeneratorMultiNodeGang:   scenarioSearchDurationForTest("0"),
			},
		}),
		framework.Reclaim,
		clock.Now,
	)
	require.NoError(t, err)

	jobBudget := budget.BeginJob()
	firstGeneratorBudget := jobBudget.BeginGenerator(constants.GeneratorNodeLocalGreedy)
	clock.Advance(60 * time.Millisecond)

	require.True(t, firstGeneratorBudget.Exhausted())
	require.Equal(t, time.Duration(0), firstGeneratorBudget.Remaining())

	secondGeneratorBudget := jobBudget.BeginGenerator(constants.GeneratorMultiNodeGang)
	clock.Advance(500 * time.Millisecond)

	require.False(t, secondGeneratorBudget.Exhausted())
	require.Equal(t, 440*time.Millisecond, secondGeneratorBudget.Remaining())
}

func TestSearchResultNilReceiver(t *testing.T) {
	var result *SearchResult

	require.Empty(t, result.Reason())
	require.False(t, result.ReducedBudget())
}

func sessionWithScenarioSearchBudgets(budgets *kaiv1.ScenarioSearchBudgets) *framework.Session {
	return &framework.Session{
		Config: &conf.SchedulerConfiguration{
			ScenarioSearchBudgets: budgets,
		},
	}
}

func scenarioSearchDurationForTest(value string) metav1.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		panic(err)
	}
	return metav1.Duration{Duration: duration}
}

func scenarioSearchDurationPtrForTest(value string) *metav1.Duration {
	return ptr.To(scenarioSearchDurationForTest(value))
}
