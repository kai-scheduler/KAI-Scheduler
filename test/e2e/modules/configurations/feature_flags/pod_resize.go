/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	kaiadmission "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/admission"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
)

// SetInPlacePodResizeValidation patches the KAI config's
// admission.inPlacePodResize settings and waits for the admission deployment
// to roll out. Passing nil for both fields clears the override, restoring
// defaults (validateQuota=true, blockUpsizeOnBoundedQueues=false).
func SetInPlacePodResizeValidation(
	ctx context.Context, testCtx *testContext.TestContext, validateQuota, blockUpsizeOnBoundedQueues *bool,
) error {
	if err := configurations.PatchKAIConfig(
		ctx, testCtx,
		func(config *kaiv1.Config) {
			if validateQuota == nil && blockUpsizeOnBoundedQueues == nil {
				if config.Spec.Admission != nil {
					config.Spec.Admission.InPlacePodResize = nil
				}
				return
			}
			if config.Spec.Admission == nil {
				config.Spec.Admission = &kaiadmission.Admission{}
			}
			config.Spec.Admission.InPlacePodResize = &kaiadmission.InPlacePodResize{
				ValidateQuota:              validateQuota,
				BlockUpsizeOnBoundedQueues: blockUpsizeOnBoundedQueues,
			}
		},
	); err != nil {
		return err
	}
	cfg := testconfig.GetConfig()
	wait.WaitForDeploymentPodsRunning(
		ctx, testCtx.ControllerClient, cfg.AdmissionDeploymentName, cfg.SystemPodsNamespace,
	)
	return nil
}
