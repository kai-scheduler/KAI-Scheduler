// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

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
	reducedBudget bool
	enteredSearch bool
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

func (r *SearchResult) EnteredSearch() bool {
	if r == nil {
		return false
	}
	return r.enteredSearch
}
