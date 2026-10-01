// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestFailedSearchBackoff(t *testing.T) {
	durations := func(values map[string]string) map[string]metav1.Duration {
		result := map[string]metav1.Duration{}
		for key, value := range values {
			result[key] = scenarioSearchDurationForTest(value)
		}
		return result
	}
	tests := []struct {
		name             string
		budgets          *kaiv1.ScenarioSearchBudgets
		action           framework.ActionType
		wantMin, wantMax time.Duration
		wantErr          bool
	}{
		{name: "no budgets", action: framework.Reclaim},
		{name: "no backoff configured", budgets: &kaiv1.ScenarioSearchBudgets{}, action: framework.Reclaim},
		{
			name: "the default applies to every action",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "10s"}),
				MaxFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "1m"}),
			},
			action:  framework.Preempt,
			wantMin: 10 * time.Second, wantMax: time.Minute,
		},
		{
			name: "an action's own setting wins over the default",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "10s", constants.ActionReclaim: "20s"}),
				MaxFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "1m", constants.ActionReclaim: "2m"}),
			},
			action:  framework.Reclaim,
			wantMin: 20 * time.Second, wantMax: 2 * time.Minute,
		},
		{
			name: "zero disables it for one action",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "10s", constants.ActionConsolidation: "0s"}),
			},
			action: framework.Consolidation,
		},
		{
			name: "the maximum defaults to the minimum",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionReclaim: "10s"}),
			},
			action:  framework.Reclaim,
			wantMin: 10 * time.Second, wantMax: 10 * time.Second,
		},
		{
			name: "a maximum below the minimum is raised to it",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionReclaim: "1m"}),
				MaxFailedSearchBackoff: durations(map[string]string{constants.ActionDefault: "10s"}),
			},
			action:  framework.Reclaim,
			wantMin: time.Minute, wantMax: time.Minute,
		},
		{
			name: "an action without a setting or default has none",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionReclaim: "10s"}),
			},
			action: framework.Preempt,
		},
		{
			name: "a negative minimum is invalid",
			budgets: &kaiv1.ScenarioSearchBudgets{
				MinFailedSearchBackoff: durations(map[string]string{constants.ActionReclaim: "-1s"}),
			},
			action:  framework.Reclaim,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minBackoff, maxBackoff, err := failedSearchBackoff(tt.budgets, tt.action)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMin, minBackoff)
			require.Equal(t, tt.wantMax, maxBackoff)
		})
	}
}

func TestRecordSearchOutcome(t *testing.T) {
	budgets := &kaiv1.ScenarioSearchBudgets{
		MinFailedSearchBackoff: map[string]metav1.Duration{constants.ActionReclaim: scenarioSearchDurationForTest("10s")},
		MaxFailedSearchBackoff: map[string]metav1.Duration{constants.ActionReclaim: scenarioSearchDurationForTest("1m")},
	}
	job := podgroup_info.NewPodGroupInfo("job")
	_, _, _, noTasksResult := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil).SolveWithResult(
		&framework.Session{}, podgroup_info.NewPodGroupInfo("job-without-tasks"))
	tests := []struct {
		name    string
		budgets *kaiv1.ScenarioSearchBudgets
		action  framework.ActionType
		result  *SearchResult
		expect  func(cacheMock *cache.MockCache)
	}{
		{
			name: "a search that exhausted its scenarios starts or extends the backoff", budgets: budgets,
			action: framework.Reclaim, result: terminalSearchResult(SearchResultGeneratorsExhausted, false),
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().RecordFailedSearch(constants.ActionReclaim, job, 10*time.Second, time.Minute)
			},
		},
		{
			name: "a solved search ends it", budgets: budgets,
			action: framework.Reclaim, result: solvedSearchResult(nil, false),
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().ClearFailedSearch(constants.ActionReclaim, job)
			},
		},
		{name: "a search stopped by its deadline is not reported", budgets: budgets, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultDeadlineExhausted, false)},
		{name: "a search not attempted is not reported", budgets: budgets, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultNotAttempted, false)},
		{name: "a search without a generator is not reported", budgets: budgets, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultNoGenerator, false)},
		{name: "a job that was not searched is not reported", budgets: budgets, action: framework.Reclaim},
		{name: "a job with no tasks to allocate is not reported", budgets: budgets, action: framework.Reclaim,
			result: noTasksResult},
		{name: "an action without a backoff reports nothing", budgets: budgets, action: framework.Preempt,
			result: terminalSearchResult(SearchResultGeneratorsExhausted, false)},
		{name: "an invalid backoff reports nothing", action: framework.Reclaim,
			budgets: &kaiv1.ScenarioSearchBudgets{MinFailedSearchBackoff: map[string]metav1.Duration{
				constants.ActionReclaim: scenarioSearchDurationForTest("-1s"),
			}},
			result: terminalSearchResult(SearchResultGeneratorsExhausted, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			cacheMock := cache.NewMockCache(controller)
			if tt.expect != nil {
				tt.expect(cacheMock)
			}
			ssn := sessionWithScenarioSearchBudgets(tt.budgets)
			ssn.Cache = cacheMock

			RecordSearchOutcome(ssn, tt.action, job, tt.result)
		})
	}
}
