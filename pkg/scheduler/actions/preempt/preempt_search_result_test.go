// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
)

func TestPreemptorOverQuotaDoesNotStopTheAction(t *testing.T) {
	require.False(t, shouldStopActionForSearchResult(solvers.NewPreemptorOverQuotaSearchResult()))
	require.True(t, shouldStopActionForSearchResult(solvers.NewNotAttemptedSearchResult()))
}

func TestPreemptionFailureReason(t *testing.T) {
	require.Equal(t, string(solvers.SearchResultPreemptorOverQuota),
		preemptionFailureReason(solvers.NewPreemptorOverQuotaSearchResult()))
	require.Equal(t, string(solvers.SearchResultNotAttempted),
		preemptionFailureReason(solvers.NewNotAttemptedSearchResult()))
	require.Equal(t, "unknown", preemptionFailureReason(nil))
}
