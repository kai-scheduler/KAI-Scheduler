// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"

// SearchResultReason describes why a scenario search stopped.
type SearchResultReason string

const (
	SearchResultSolved              SearchResultReason = "solved"
	SearchResultDeadlineExhausted   SearchResultReason = "deadline_exhausted"
	SearchResultGeneratorsExhausted SearchResultReason = "generators_exhausted"
	SearchResultNoGenerator         SearchResultReason = "no_generator"
	SearchResultNotAttempted        SearchResultReason = "not_attempted"
)

// SearchResult records the outcome and budget state of a scenario search attempt.
type SearchResult struct {
	reason        SearchResultReason
	solution      *solutionResult
	reducedBudget bool
	metricResult  string
}

func (r *SearchResult) Reason() SearchResultReason {
	if r == nil {
		return ""
	}
	return r.reason
}

func (r *SearchResult) ReducedBudget() bool {
	if r == nil {
		return false
	}
	return r.reducedBudget
}

func (r *SearchResult) scenarioSearchMetricResult() string {
	if r == nil {
		return ""
	}
	if r.metricResult != "" {
		return r.metricResult
	}
	return string(r.reason)
}

func (r *SearchResult) ScenarioSearchUnresolved() *podgroup_info.ScenarioSearchUnresolved {
	if r == nil {
		return nil
	}

	var reason podgroup_info.ScenarioSearchResultReason
	switch r.reason {
	case SearchResultDeadlineExhausted:
		reason = podgroup_info.ScenarioSearchResultDeadlineExhausted
	case SearchResultGeneratorsExhausted:
		reason = podgroup_info.ScenarioSearchResultGeneratorsExhausted
	case SearchResultNoGenerator:
		reason = podgroup_info.ScenarioSearchResultNoGenerator
	case SearchResultNotAttempted:
		reason = podgroup_info.ScenarioSearchResultNotAttempted
	default:
		return nil
	}

	return &podgroup_info.ScenarioSearchUnresolved{
		Reason:        reason,
		ReducedBudget: r.reducedBudget,
	}
}

func RecordScenarioSearchUnresolved(job *podgroup_info.PodGroupInfo, result *SearchResult) {
	unresolved := result.ScenarioSearchUnresolved()
	if unresolved == nil {
		return
	}
	job.SetScenarioSearchUnresolved(unresolved.Reason, unresolved.ReducedBudget)
}

// NewNotAttemptedSearchResult returns a terminal result for callers that skip solver entry.
func NewNotAttemptedSearchResult() *SearchResult {
	return terminalSearchResult(SearchResultNotAttempted, false)
}

func solvedSearchResult(solution *solutionResult, reducedBudget bool) *SearchResult {
	return &SearchResult{
		reason:        SearchResultSolved,
		solution:      solution,
		reducedBudget: reducedBudget,
	}
}

func terminalSearchResult(reason SearchResultReason, reducedBudget bool) *SearchResult {
	return &SearchResult{
		reason:        reason,
		reducedBudget: reducedBudget,
	}
}
