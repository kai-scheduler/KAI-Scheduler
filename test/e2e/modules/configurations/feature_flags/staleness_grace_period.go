/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
	"k8s.io/utils/ptr"
)

func SetDefaultStalenessGracePeriod(
	ctx context.Context, testCtx *testcontext.TestContext, value *string,
) error {
	var targetValue *string = nil
	if value != nil {
		targetValue = ptr.To(*value)
	}
	if err := configurations.SetShardArg(ctx, testCtx, "default", "default-staleness-grace-period", targetValue); err != nil {
		return err
	}
	cfg := testconfig.GetConfig()
	wait.WaitForDeploymentPodsRunning(ctx, testCtx.ControllerClient, cfg.SchedulerDeploymentName, cfg.SystemPodsNamespace)
	return nil
}
