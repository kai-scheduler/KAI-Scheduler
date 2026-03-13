/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	"k8s.io/utils/ptr"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	testContext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func SetPluginEnabled(
	ctx context.Context, testCtx *testContext.TestContext, pluginName string, enabled bool,
) error {
	return patchPlugin(ctx, testCtx, func(shard *kaiv1.SchedulingShard) {
		if shard.Spec.Plugins == nil {
			shard.Spec.Plugins = make(map[string]kaiv1.PluginConfig)
		}
		config := shard.Spec.Plugins[pluginName]
		config.Enabled = ptr.To(enabled)
		shard.Spec.Plugins[pluginName] = config
	})
}

func UnsetPlugin(
	ctx context.Context, testCtx *testContext.TestContext, pluginName string,
) error {
	return patchPlugin(ctx, testCtx, func(shard *kaiv1.SchedulingShard) {
		delete(shard.Spec.Plugins, pluginName)
	})
}

func patchPlugin(
	ctx context.Context, testCtx *testContext.TestContext,
	mutateFn func(shard *kaiv1.SchedulingShard),
) error {
	if err := configurations.PatchSchedulingShard(
		ctx, testCtx, "default",
		mutateFn,
	); err != nil {
		return err
	}
	wait.WaitForDeploymentPodsRunning(
		ctx, testCtx.ControllerClient, constant.SchedulerDeploymentName, constants.DefaultKAINamespace,
	)
	return nil
}
