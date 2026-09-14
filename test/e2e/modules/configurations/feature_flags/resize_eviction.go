// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"

	"k8s.io/utils/ptr"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

// SetResizeEvictionAction enables or disables the resizeeviction scheduler action on the
// default shard. Disabling removes the override, restoring the default (off).
func SetResizeEvictionAction(ctx context.Context, testCtx *testContext.TestContext, enabled bool) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if enabled {
				if shard.Spec.Actions == nil {
					shard.Spec.Actions = map[string]kaiv1.ActionConfig{}
				}
				shard.Spec.Actions["resizeeviction"] = kaiv1.ActionConfig{Enabled: ptr.To(true)}
			} else {
				delete(shard.Spec.Actions, "resizeeviction")
			}
			shard.Status = kaiv1.SchedulingShardStatus{}
		},
	)
}
