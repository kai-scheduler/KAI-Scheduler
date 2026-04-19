/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"k8s.io/utils/ptr"
)

const (
	binpackStrategy = "binpack"
	SpreadStrategy  = "spread"
	DefaultStrategy = binpackStrategy
	gpuResource     = "gpu"
	cpuResource     = "cpu"
)

func SetPlacementStrategy(
	ctx context.Context, testCtx *testContext.TestContext, strategy string,
) error {
	if err := configurations.PatchSchedulingShard(
		ctx, testCtx, "default",
		func(shard *kaiv1.SchedulingShard) {
			shard.Spec.PlacementStrategy.CPU = ptr.To(strategy)
			shard.Spec.PlacementStrategy.GPU = ptr.To(strategy)
		},
	); err != nil {
		return err
	}
	wait.WaitForDeploymentPodsRunning(
		ctx, testCtx.ControllerClient, constant.SchedulerDeploymentName, constants.DefaultKAINamespace,
	)
	return nil
}
