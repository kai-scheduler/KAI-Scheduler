// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"
	"strconv"

	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
)

const deferredResizePreemptionArg = "deferred-resize-preemption"

// SetDeferredResizePreemption sets the scheduler's deferred-resize-preemption flag on the
// default shard. nil removes the override, restoring the default (disabled).
func SetDeferredResizePreemption(ctx context.Context, testCtx *testContext.TestContext, enabled *bool) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if enabled == nil {
				delete(shard.Spec.Args, deferredResizePreemptionArg)
			} else {
				if shard.Spec.Args == nil {
					shard.Spec.Args = map[string]string{}
				}
				shard.Spec.Args[deferredResizePreemptionArg] = strconv.FormatBool(*enabled)
			}
			shard.Status = kaiv1.SchedulingShardStatus{}
		},
	)
}
