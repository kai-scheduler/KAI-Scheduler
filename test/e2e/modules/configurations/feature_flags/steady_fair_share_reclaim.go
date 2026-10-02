// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package feature_flags

import (
	"context"
	"maps"
	"strconv"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

const (
	proportionPluginName      = "proportion"
	steadyFairShareReclaimArg = "steadyFairShareReclaim"
)

// SetSteadyFairShareReclaim sets the proportion plugin's steadyFairShareReclaim argument on the default
// shard, or removes it when value is nil. Plugin arguments replace the plugin's default arguments, so
// the kValue the shard derives from spec.kValue is kept explicitly.
func SetSteadyFairShareReclaim(ctx context.Context, testCtx *testContext.TestContext, value *bool) error {
	return patchShard(
		ctx, testCtx, defaultShardName,
		func(shard *kaiv1.SchedulingShard) {
			if shard.Spec.Plugins == nil {
				shard.Spec.Plugins = map[string]kaiv1.PluginConfig{}
			}
			plugin := shard.Spec.Plugins[proportionPluginName]
			arguments := maps.Clone(plugin.Arguments)
			if arguments == nil {
				arguments = map[string]string{}
			}
			if value == nil {
				delete(arguments, steadyFairShareReclaimArg)
			} else {
				arguments[steadyFairShareReclaimArg] = strconv.FormatBool(*value)
			}
			if _, found := arguments["kValue"]; !found && shard.Spec.KValue != nil && len(arguments) > 0 {
				arguments["kValue"] = strconv.FormatFloat(*shard.Spec.KValue, 'f', -1, 64)
			}
			plugin.Arguments = arguments
			if len(arguments) == 0 {
				plugin.Arguments = nil
			}
			shard.Spec.Plugins[proportionPluginName] = plugin
			if plugin.Arguments == nil && plugin.Enabled == nil && plugin.Priority == nil {
				delete(shard.Spec.Plugins, proportionPluginName)
			}
			shard.Status = kaiv1.SchedulingShardStatus{}
		},
	)
}
