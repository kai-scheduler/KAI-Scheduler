/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func UnsetKnativeGangScheduling(ctx context.Context, testCtx *testcontext.TestContext) error {
	return SetKnativeGangScheduling(ctx, testCtx, nil)
}

func SetKnativeGangScheduling(ctx context.Context, testCtx *testcontext.TestContext, value *bool) error {
	return wait.PatchSystemDeploymentFeatureFlags(
		ctx,
		testCtx.KubeClientset,
		testCtx.ControllerClient,
		constant.SystemPodsNamespace,
		"podgrouper",
		"podgrouper",
		func(args []string) []string {
			return genericArgsUpdater(args, "--knative-gang-schedule=", value)
		},
	)
}
