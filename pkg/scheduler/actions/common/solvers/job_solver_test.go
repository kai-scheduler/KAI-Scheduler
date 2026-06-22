// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchMaxSolvableKSkipsFullProbe(t *testing.T) {
	probes := []int{}

	maxSolvedK, result := searchMaxSolvableK(4, func(k int) *SearchResult {
		probes = append(probes, k)
		return solvedSearchResult(&solutionResult{solved: true}, false)
	})

	require.Equal(t, 3, maxSolvedK)
	require.Nil(t, result)
	require.Equal(t, []int{1, 2, 3}, probes)
}

func TestSearchMaxSolvableKSkipsSingleTaskFullProbe(t *testing.T) {
	probes := []int{}

	maxSolvedK, result := searchMaxSolvableK(1, func(k int) *SearchResult {
		probes = append(probes, k)
		return solvedSearchResult(&solutionResult{solved: true}, false)
	})

	require.Equal(t, 0, maxSolvedK)
	require.Nil(t, result)
	require.Empty(t, probes)
}
