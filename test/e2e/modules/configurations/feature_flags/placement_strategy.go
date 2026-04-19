/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"
	"fmt"
	"slices"
	"strings"

	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

const (
	binpackStrategy = "binpack"
	spreadStrategy  = "spread"
	gpuResource     = "gpu"
	cpuResource     = "cpu"
)

func SetPlacementStrategy(
	ctx context.Context, testCtx *testContext.TestContext, strategy string,
) error {
	return updateKaiSchedulerConfigMap(ctx, testCtx, func() (*config, error) {
		placementArguments := map[string]string{
			gpuResource: strategy, cpuResource: strategy,
		}

		innerConfig := config{}

		actions := []string{"allocate"}
		if placementArguments[gpuResource] != spreadStrategy && placementArguments[cpuResource] != spreadStrategy {
			actions = append(actions, "consolidation")
		}
		actions = append(actions, []string{"reclaim", "preempt", "stalegangeviction"}...)

		innerConfig.Actions = strings.Join(actions, ", ")

		innerConfig.Tiers = slices.Clone(defaultKaiSchedulerPlugins)
		innerConfig.Tiers[0].Plugins = append(
			innerConfig.Tiers[0].Plugins,
			plugin{Name: fmt.Sprintf("gpu%s", strings.Replace(placementArguments[gpuResource], "bin", "", 1))},
			plugin{
				Name:      "nodeplacement",
				Arguments: placementArguments,
			},
		)

		if placementArguments[gpuResource] == binpackStrategy {
			innerConfig.Tiers[0].Plugins = append(
				innerConfig.Tiers[0].Plugins,
				plugin{Name: "gpusharingorder"},
			)
		}

		return &innerConfig, nil
	})
}
