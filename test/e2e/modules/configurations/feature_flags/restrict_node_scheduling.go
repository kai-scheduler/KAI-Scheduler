/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"k8s.io/utils/ptr"
)

func SetRestrictNodeScheduling(
	value *bool, testCtx *testContext.TestContext, ctx context.Context,
) error {
	var targetValue *string = nil
	if value != nil {
		targetValue = ptr.To(fmt.Sprint(*value))
	}
	if err := configurations.SetShardArg(ctx, testCtx, "default", "restrict-node-scheduling", targetValue); err != nil {
		return err
	}
	cfg := testconfig.GetConfig()
	wait.WaitForDeploymentPodsRunning(ctx, testCtx.ControllerClient, cfg.SchedulerDeploymentName, cfg.SystemPodsNamespace)
	return nil
}
