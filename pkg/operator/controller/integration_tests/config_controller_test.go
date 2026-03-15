// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package integration_tests

import (
	"context"
	"os"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv1binder "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/binder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1common "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

const (
	githubRoot = "../../../../"
	repository = "ghcr.io/kai-scheduler/kai-scheduler"
	tag        = "latest"
)

var _ = Describe("KAIConfigController", Ordered, func() {
	var (
		kaiConfig *kaiv1.Config
	)

	BeforeAll(func() {
		kaiConfig = &kaiv1.Config{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.DefaultKAIConfigSingeltonInstanceName,
			},
			Spec: kaiv1.ConfigSpec{
				Namespace: "kai-scheduler",
				Binder: &kaiv1binder.Binder{
					Service: &v1common.Service{
						Enabled: ptr.To(true),
					},
				},
			},
		}
		os.Setenv(v1common.DefaultRepositoryEnvVarName, repository)
		os.Setenv(v1common.DefaultTagEnvVarName, tag)

		Expect(k8sClient.Create(context.Background(), kaiConfig)).To(Succeed())
	})

	AfterAll(func() {
		Expect(k8sClient.Delete(context.Background(), kaiConfig)).To(Succeed())
		os.Unsetenv(v1common.DefaultRepositoryEnvVarName)
		os.Unsetenv(v1common.DefaultTagEnvVarName)
	})

	Context("Watching ClusterPolicy", Ordered, func() {
		It("Updates binder deployment when ClusterPolicy changes", func(ctx context.Context) {
			var binderDeploymentGeneration int64
			Eventually(func(g Gomega) {
				binderDeployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binder",
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binderDeployment), binderDeployment)).To(Succeed())
				binderDeploymentGeneration = binderDeployment.Generation
			}, "10s", "200ms").Should(Succeed())

			Expect(k8sClient.Create(ctx, &nvidiav1.ClusterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example-cluster-policy",
				},
				Spec: nvidiav1.ClusterPolicySpec{
					Operator: nvidiav1.OperatorSpec{
						DefaultRuntime: nvidiav1.Docker,
					},
					CDI: nvidiav1.CDIConfigSpec{
						Enabled: ptr.To(true),
						Default: ptr.To(true),
					},
				},
			})).To(Succeed())

			Eventually(func(g Gomega) bool {
				binderDeployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binder",
						Namespace: kaiConfig.Spec.Namespace,
					},
				}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(binderDeployment), binderDeployment)).To(Succeed())

				return binderDeploymentGeneration < binderDeployment.Generation
			}, "10s", "200ms").Should(BeTrue())

			Eventually(func(g Gomega) bool {
				updatedConfig := &kaiv1.Config{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: constants.DefaultKAIConfigSingeltonInstanceName}, updatedConfig)).To(Succeed())

				for _, condition := range updatedConfig.Status.Conditions {
					if condition.Type == string(kaiv1.ConditionTypeDeployed) && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, "10s", "200ms").Should(BeTrue())
		})
	})

})
