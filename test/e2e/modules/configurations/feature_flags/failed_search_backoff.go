// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

// SetFailedSearchBackoff sets the failed-search backoff of action on the default shard. A nil bound
// is removed.
func SetFailedSearchBackoff(
	ctx context.Context, testCtx *testContext.TestContext, action string, minBackoff, maxBackoff *metav1.Duration,
) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if shard.Spec.ScenarioSearchBudgets == nil {
				shard.Spec.ScenarioSearchBudgets = &kaiv1.ScenarioSearchBudgets{}
			}
			budgets := shard.Spec.ScenarioSearchBudgets
			budgets.MinFailedSearchBackoff = withActionDuration(budgets.MinFailedSearchBackoff, action, minBackoff)
			budgets.MaxFailedSearchBackoff = withActionDuration(budgets.MaxFailedSearchBackoff, action, maxBackoff)
		},
	)
}

func withActionDuration(
	durations map[string]metav1.Duration, action string, duration *metav1.Duration,
) map[string]metav1.Duration {
	if duration == nil {
		delete(durations, action)
		return durations
	}
	if durations == nil {
		durations = map[string]metav1.Duration{}
	}
	durations[action] = *duration
	return durations
}
