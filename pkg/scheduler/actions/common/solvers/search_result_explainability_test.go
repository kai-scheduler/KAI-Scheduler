// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

func TestSearchResultScenarioSearchUnresolved(t *testing.T) {
	tests := []struct {
		name           string
		result         *SearchResult
		expectedReason podgroup_info.ScenarioSearchResultReason
		reducedBudget  bool
	}{
		{
			name:           "deadline exhausted",
			result:         terminalSearchResult(SearchResultDeadlineExhausted, false),
			expectedReason: podgroup_info.ScenarioSearchResultDeadlineExhausted,
		},
		{
			name:           "generators exhausted",
			result:         terminalSearchResult(SearchResultGeneratorsExhausted, false),
			expectedReason: podgroup_info.ScenarioSearchResultGeneratorsExhausted,
		},
		{
			name:           "no generator",
			result:         terminalSearchResult(SearchResultNoGenerator, false),
			expectedReason: podgroup_info.ScenarioSearchResultNoGenerator,
		},
		{
			name:           "not attempted",
			result:         terminalSearchResult(SearchResultNotAttempted, false),
			expectedReason: podgroup_info.ScenarioSearchResultNotAttempted,
		},
		{
			name:           "reduced budget",
			result:         terminalSearchResult(SearchResultDeadlineExhausted, true),
			expectedReason: podgroup_info.ScenarioSearchResultDeadlineExhausted,
			reducedBudget:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unresolved := tt.result.ScenarioSearchUnresolved()

			require.NotNil(t, unresolved)
			require.Equal(t, tt.expectedReason, unresolved.Reason)
			require.Equal(t, tt.reducedBudget, unresolved.ReducedBudget)
		})
	}
}

func TestSearchResultScenarioSearchUnresolvedIgnoresSolvedAndNilResults(t *testing.T) {
	require.Nil(t, solvedSearchResult(&solutionResult{solved: true}, false).ScenarioSearchUnresolved())

	var result *SearchResult
	require.Nil(t, result.ScenarioSearchUnresolved())
}
