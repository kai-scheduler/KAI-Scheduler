/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"k8s.io/utils/ptr"
)

func UnsetKnativeGangScheduling(ctx context.Context, testCtx *testcontext.TestContext) error {
	return SetKnativeGangScheduling(ctx, testCtx, nil)
}

func SetKnativeGangScheduling(ctx context.Context, testCtx *testcontext.TestContext, value *bool) error {
	var targetValue *string = nil
	if value != nil {
		targetValue = ptr.To(fmt.Sprint(*value))
	}
	if err := configurations.SetShardArg(ctx, testCtx, "default", "knative-gang-schedule", targetValue); err != nil {
		return err
	}
	wait.WaitForDeploymentPodsRunning(ctx, testCtx.ControllerClient, constant.SchedulerDeploymentName, constants.DefaultKAINamespace)
	return nil
}
