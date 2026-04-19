/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func SetRestrictNodeScheduling(
	value *bool, testCtx *testContext.TestContext, ctx context.Context,
) error {
	return wait.PatchSystemDeploymentFeatureFlags(
		ctx,
		testCtx.KubeClientset,
		testCtx.ControllerClient,
		constant.SystemPodsNamespace,
		constant.SchedulerDeploymentName,
		constant.SchedulerContainerName,
		func(args []string) []string {
			return genericArgsUpdater(args, "--restrict-node-scheduling=", value)
		},
	)
}
