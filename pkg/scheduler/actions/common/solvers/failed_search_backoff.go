// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
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
	minBackoff, maxBackoff, err := conf.FailedSearchBackoff(
		ssn.SchedulerParams.MinFailedSearchBackoff, ssn.SchedulerParams.MaxFailedSearchBackoff,
		scenarioSearchActionKey(action),
	)
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
