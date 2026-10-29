// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"
	"maps"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
)

// SetFailedSearchBackoff sets action's failed-search backoff on the default shard, through the scheduler's
// --min-failed-search-backoff and --max-failed-search-backoff args; nil removes it.
func SetFailedSearchBackoff(
	ctx context.Context, testCtx *testContext.TestContext, action string, minBackoff, maxBackoff *metav1.Duration,
) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			shard.Spec.Args = withActionDuration(shard.Spec.Args, conf.MinFailedSearchBackoffFlag, action, minBackoff)
			shard.Spec.Args = withActionDuration(shard.Spec.Args, conf.MaxFailedSearchBackoffFlag, action, maxBackoff)
		},
	)
}

// withActionDuration sets action's duration in the arg's action=duration list, or removes it.
func withActionDuration(args map[string]string, arg, action string, duration *metav1.Duration) map[string]string {
	durations := map[string]string{}
	for _, pair := range strings.Split(args[arg], ",") {
		if key, value, found := strings.Cut(pair, "="); found {
			durations[key] = value
		}
	}
	if duration == nil {
		delete(durations, action)
	} else {
		durations[action] = duration.Duration.String()
	}
	if args == nil {
		args = map[string]string{}
	}
	if len(durations) == 0 {
		delete(args, arg)
		return args
	}
	pairs := make([]string, 0, len(durations))
	for _, key := range slices.Sorted(maps.Keys(durations)) {
		pairs = append(pairs, key+"="+durations[key])
	}
	args[arg] = strings.Join(pairs, ",")
	return args
}
