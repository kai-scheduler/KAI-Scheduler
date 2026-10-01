// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// RecordSearchOutcome reports a scenario search for job to the cache when action has a failed-search
// backoff configured: a search that exhausted its scenarios without a solution starts or extends the
// job's backoff, and a solved one ends it. Searches stopped by a budget are not reported.
func RecordSearchOutcome(
	ssn *framework.Session, action framework.ActionType, job *podgroup_info.PodGroupInfo, result *SearchResult,
) {
	if result == nil {
		return
	}
	minBackoff, maxBackoff, err := failedSearchBackoff(scenarioSearchBudgets(ssn), action)
	if err != nil {
		log.InfraLogger.Errorf("Invalid failed search backoff for %s: %v", action, err)
		return
	}
	if minBackoff == 0 {
		return
	}
	// The metric result leaves out a job with no tasks to allocate, which the solver reports as
	// exhausted without searching.
	switch result.scenarioSearchMetricResult() {
	case string(SearchResultSolved):
		ssn.Cache.ClearFailedSearch(string(action), job)
	case string(SearchResultGeneratorsExhausted):
		ssn.Cache.RecordFailedSearch(string(action), job, minBackoff, maxBackoff)
	}
}

// failedSearchBackoff returns the bounds of action's failed-search backoff, zero when it is disabled.
// The maximum defaults to, and is at least, the minimum.
func failedSearchBackoff(
	budgets *kaiv1.ScenarioSearchBudgets, action framework.ActionType,
) (time.Duration, time.Duration, error) {
	if budgets == nil {
		return 0, 0, nil
	}
	minBackoff, err := actionDuration("minFailedSearchBackoff", budgets.MinFailedSearchBackoff, action)
	if err != nil || minBackoff == 0 {
		return 0, 0, err
	}
	maxBackoff, err := actionDuration("maxFailedSearchBackoff", budgets.MaxFailedSearchBackoff, action)
	if err != nil {
		return 0, 0, err
	}
	return minBackoff, max(minBackoff, maxBackoff), nil
}

// actionDuration returns the duration set for action, or else the default one, or zero.
func actionDuration(
	fieldName string, durationValues map[string]metav1.Duration, action framework.ActionType,
) (time.Duration, error) {
	durations, err := parseDurationMap(fieldName, durationValues)
	if err != nil {
		return 0, err
	}
	if duration, found := durations[scenarioSearchActionKey(action)]; found {
		return duration, nil
	}
	return durations[constants.ActionDefault], nil
}
