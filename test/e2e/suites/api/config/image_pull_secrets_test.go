/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package config

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = Describe("Image Pull Secrets", Ordered, func() {
	var (
		testCtx             *testcontext.TestContext
		originalPullSecrets []string
		setupComplete       bool
		testSecretName      = "test-image-pull-secret"
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		kaiConfig := &kaiv1.Config{}
		Expect(testCtx.ControllerClient.Get(ctx,
			runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName},
			kaiConfig)).To(Succeed())
		originalPullSecrets = kaiConfig.Spec.Global.ImagePullSecrets
		setupComplete = true
	})

	AfterAll(func(ctx context.Context) {
		if !setupComplete {
			return
		}
		err := configurations.PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) {
			conf.Spec.Global.ImagePullSecrets = originalPullSecrets
		})
		Expect(err).To(Succeed())
		wait.ForKAIConfigStatusOK(ctx, testCtx.ControllerClient)
	})

	It("should propagate additionalImagePullSecrets to deployment pods", func(ctx context.Context) {
		err := configurations.PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) {
			conf.Spec.Global.ImagePullSecrets = append(conf.Spec.Global.ImagePullSecrets, testSecretName)
		})
		Expect(err).To(Succeed())
		wait.ForKAIConfigStatusOK(ctx, testCtx.ControllerClient)

		cfg := testconfig.GetConfig()
		deployments := &appsv1.DeploymentList{}
		Expect(testCtx.ControllerClient.List(ctx, deployments,
			runtimeClient.InNamespace(cfg.SystemPodsNamespace),
		)).To(Succeed())
		Expect(deployments.Items).NotTo(BeEmpty(), "expected at least one deployment in %s", cfg.SystemPodsNamespace)

		operatorManagedDeployments := filterManagedDeployments(deployments.Items)
		Expect(operatorManagedDeployments).NotTo(BeEmpty(), "expected at least one operator-managed deployment")

		for _, deployment := range operatorManagedDeployments {
			Expect(deployment.Spec.Template.Spec.ImagePullSecrets).To(
				ContainElement(v1.LocalObjectReference{Name: testSecretName}),
				fmt.Sprintf("deployment %s missing imagePullSecret %s", deployment.Name, testSecretName),
			)
		}
	})
})

func filterManagedDeployments(deployments []appsv1.Deployment) []appsv1.Deployment {
	var managed []appsv1.Deployment
	for _, d := range deployments {
		if _, hasAppLabel := d.Labels[constants.AppLabelName]; hasAppLabel {
			if d.Name == "kai-operator" {
				continue
			}
			managed = append(managed, d)
		}
	}
	return managed
}
