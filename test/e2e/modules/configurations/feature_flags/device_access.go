/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiadmission "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/admission"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
)

const blockNvidiaVisibleDevicesArg = "--block-nvidia-visible-devices=true"

// SetBlockNvidiaVisibleDevices toggles the admission visible-devices validation plugin via the KAI Config CR
// and blocks until the operator has reconciled the flag into the admission deployment args
// and the rollout has completed, so callers can rely on the new behavior immediately.
func SetBlockNvidiaVisibleDevices(ctx context.Context, testCtx *testContext.TestContext, value bool) error {
	if err := configurations.PatchKAIConfig(ctx, testCtx, func(kaiConfig *kaiv1.Config) {
		if kaiConfig.Spec.Admission == nil {
			kaiConfig.Spec.Admission = &kaiadmission.Admission{}
		}
		kaiConfig.Spec.Admission.BlockNvidiaVisibleDevices = ptr.To(value)
	}); err != nil {
		return err
	}

	cfg := testconfig.GetConfig()
	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(testCtx.ControllerClient.Get(ctx, client.ObjectKey{
			Namespace: cfg.SystemPodsNamespace,
			Name:      cfg.AdmissionDeploymentName,
		}, deployment)).To(Succeed())

		args := deployment.Spec.Template.Spec.Containers[0].Args
		if value {
			g.Expect(args).To(ContainElement(blockNvidiaVisibleDevicesArg))
		} else {
			g.Expect(args).NotTo(ContainElement(blockNvidiaVisibleDevicesArg))
		}

		// Ensure the rollout to the reconciled spec has fully completed (old pods gone,
		// new pods available) so no stale admission pod serves the webhook.
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		g.Expect(deployment.Status.ObservedGeneration).To(Equal(deployment.Generation))
		g.Expect(deployment.Status.UpdatedReplicas).To(Equal(desired))
		g.Expect(deployment.Status.AvailableReplicas).To(Equal(desired))
		g.Expect(deployment.Status.Replicas).To(Equal(desired))
	}, 3*time.Minute, 2*time.Second).Should(Succeed())

	return nil
}
