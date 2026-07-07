// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"k8s.io/utils/ptr"
)

const numaPluginName = "numa"

// EnableNUMA turns on the numa scheduler plugin on the default shard with the given arguments
// (e.g. reconstructAvailable, ignoreList). Passing nil arguments keeps the plugin's defaults.
func EnableNUMA(ctx context.Context, testCtx *testContext.TestContext, arguments map[string]string) error {
	return setNUMA(ctx, testCtx, ptr.To(true), arguments)
}

// DisableNUMA turns off the numa plugin on the default shard.
func DisableNUMA(ctx context.Context, testCtx *testContext.TestContext) error {
	return setNUMA(ctx, testCtx, ptr.To(false), nil)
}

func setNUMA(
	ctx context.Context, testCtx *testContext.TestContext, enabled *bool, arguments map[string]string,
) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if shard.Spec.Plugins == nil {
				shard.Spec.Plugins = map[string]kaiv1.PluginConfig{}
			}
			shard.Spec.Plugins[numaPluginName] = kaiv1.PluginConfig{
				Enabled:   enabled,
				Arguments: arguments,
			}
			shard.Status = kaiv1.SchedulingShardStatus{}
		},
	)
}
