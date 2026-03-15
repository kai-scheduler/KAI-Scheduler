/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package configurations

import (
	"context"
	"fmt"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	. "github.com/onsi/ginkgo/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DisableScheduler(ctx context.Context, testCtx *testcontext.TestContext) {
	err := PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) { conf.Spec.Scheduler.Service.Enabled = ptr.To(false) })
	if err != nil {
		Fail(fmt.Sprintf("Failed to patch kai-config: %v", err))
	}

	cfg := testconfig.GetConfig()
	wait.ForPodsToBeDeleted(
		ctx, testCtx.ControllerClient,
		client.InNamespace(cfg.SystemPodsNamespace),
		client.MatchingLabels{constants.AppLabelName: cfg.SchedulerDeploymentName},
	)
}

func EnableScheduler(ctx context.Context, testCtx *testcontext.TestContext) {
	err := PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) { conf.Spec.Scheduler.Service.Enabled = ptr.To(true) })
	if err != nil {
		Fail(fmt.Sprintf("Failed to patch kai-config: %v", err))
	}
	wait.ForRunningSystemComponentEvent(ctx, testCtx.ControllerClient, testconfig.GetConfig().SchedulerDeploymentName)
}
