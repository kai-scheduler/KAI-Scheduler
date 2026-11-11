// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/api/constants"
)

func TestRecordSearchOutcome(t *testing.T) {
	backoff := failedSearchBackoffFlags{
		min: map[string]string{constants.ActionReclaim: "10s"},
		max: map[string]string{constants.ActionReclaim: "1m"},
	}
	job := podgroup_info.NewPodGroupInfo("job")
	_, _, _, noTasksResult := NewJobsSolver(nil, nil, nil, framework.Reclaim, nil).SolveWithResult(
		&framework.Session{}, podgroup_info.NewPodGroupInfo("job-without-tasks"))
	tests := []struct {
		name    string
		backoff failedSearchBackoffFlags
		action  framework.ActionType
		result  *SearchResult
		expect  func(cacheMock *cache.MockCache)
	}{
		{
			name: "a search that exhausted its scenarios starts or extends the backoff", backoff: backoff,
			action: framework.Reclaim, result: terminalSearchResult(SearchResultGeneratorsExhausted, false),
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().RecordFailedSearch(constants.ActionReclaim, job, 10*time.Second, time.Minute)
			},
		},
		{
			name: "a solved search ends it", backoff: backoff,
			action: framework.Reclaim, result: solvedSearchResult(nil, false),
			expect: func(cacheMock *cache.MockCache) {
				cacheMock.EXPECT().ClearFailedSearch(constants.ActionReclaim, job)
			},
		},
		{name: "a search stopped by its deadline is not reported", backoff: backoff, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultDeadlineExhausted, false)},
		{name: "a search not attempted is not reported", backoff: backoff, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultNotAttempted, false)},
		{name: "a search without a generator is not reported", backoff: backoff, action: framework.Reclaim,
			result: terminalSearchResult(SearchResultNoGenerator, false)},
		{name: "a job that was not searched is not reported", backoff: backoff, action: framework.Reclaim},
		{name: "a job with no tasks to allocate is not reported", backoff: backoff, action: framework.Reclaim,
			result: noTasksResult},
		{name: "an action without a backoff reports nothing", backoff: backoff, action: framework.Preempt,
			result: terminalSearchResult(SearchResultGeneratorsExhausted, false)},
		{name: "an invalid backoff reports nothing", action: framework.Reclaim,
			backoff: failedSearchBackoffFlags{min: map[string]string{constants.ActionReclaim: "-1s"}},
			result:  terminalSearchResult(SearchResultGeneratorsExhausted, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			cacheMock := cache.NewMockCache(controller)
			if tt.expect != nil {
				tt.expect(cacheMock)
			}
			ssn := &framework.Session{SchedulerParams: conf.SchedulerParams{
				MinFailedSearchBackoff: tt.backoff.min, MaxFailedSearchBackoff: tt.backoff.max,
			}}
			ssn.Cache = cacheMock

			RecordSearchOutcome(ssn, tt.action, job, tt.result)
		})
	}
}

// failedSearchBackoffFlags are the values of --min-failed-search-backoff and --max-failed-search-backoff.
type failedSearchBackoffFlags struct {
	min, max map[string]string
}
