// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_controller

import (
	"context"
	"testing"

	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"golang.org/x/exp/maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	test_utils "github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common/test_utils"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestQueueController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "QueueController operand Suite")
}

var _ = Describe("QueueController", func() {
	Describe("DesiredState", func() {
		var (
			fakeKubeClient client.Client
			qc             *QueueController
			kaiConfig      *kaiv1.Config
		)
		BeforeEach(func(ctx context.Context) {
			testScheme := scheme.Scheme
			Expect(kaiv1.AddToScheme(testScheme)).To(Succeed())
			Expect(apiextensionsv1.AddToScheme(testScheme)).To(Succeed())

			fakeKubeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()
			qc = &QueueController{}
			kaiConfig = kaiConfigForQueueController()
		})

		Context("Deployment", func() {
			It("should return a Deployment in the objects list", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Service.K8sClientConfig.QPS = ptr.To(42)
				kaiConfig.Spec.QueueController.Service.K8sClientConfig.Burst = ptr.To(84)

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				Expect(deployment).NotTo(BeNil())
				Expect(deployment.Name).To(Equal(defaultResourceName))
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
					"--qps", "42", "--burst", "84",
				))
			})

			It("the deployment should keep labels from existing deployment", func(ctx context.Context) {
				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				maps.Copy(deployment.Labels, map[string]string{
					"foo": "bar",
				})
				maps.Copy(deployment.Spec.Template.Labels, map[string]string{
					"run": "ai",
				})
				Expect(fakeKubeClient.Create(ctx, deployment)).To(Succeed())

				objects, err = qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				deploymentT = test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment = *deploymentT
				Expect(deployment.Labels).To(HaveKeyWithValue("foo", "bar"))
				Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("run", "ai"))
			})
		})

		Context("Validation Webhooks", func() {
			It("should return validating webhooks in the objects list", func(ctx context.Context) {
				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				validatingWebhookConfigurations := test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)
				Expect(len(validatingWebhookConfigurations)).To(Equal(len(constants.QueueValidatedVersions())))
				Expect(validatingWebhookConfigurations).NotTo(BeNil())

				var validatedVersions []string
				for _, validatingWebhookConfiguration := range validatingWebhookConfigurations {
					validatedVersions = append(validatedVersions, validatingWebhookConfiguration.Webhooks[0].Rules[0].APIVersions...)
				}

				Expect(len(validatedVersions)).To(Equal(len(constants.QueueValidatedVersions())))
				Expect(validatedVersions).To(ConsistOf(constants.QueueValidatedVersions()))
			})

			It("should use the same secret in all webhook configurations", func(ctx context.Context) {
				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				secret := *test_utils.FindTypeInObjects[*corev1.Secret](objects)
				Expect(secret).NotTo(BeNil())
				Expect(secret.Data).To(HaveKey("tls.crt"))
				Expect(secret.Data).To(HaveKey("tls.key"))

				validatingWebhookConfigurations := test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)
				Expect(len(validatingWebhookConfigurations)).To(BeNumerically(">", 0))

				for _, webhookConfig := range validatingWebhookConfigurations {
					Expect(webhookConfig.Webhooks).To(HaveLen(1))
					// Check that the webhook has a CABundle and it's not empty
					Expect(webhookConfig.Webhooks[0].ClientConfig.CABundle).To(Equal(secret.Data[certKey]))
					// Check that the service name is correct
					Expect(webhookConfig.Webhooks[0].ClientConfig.Service.Name).To(Equal("queue-controller"))
				}
			})

			It("should not return validating webhooks when flag is off", func(ctx context.Context) {
				noValidationKAIConfig := kaiConfig.DeepCopy()
				noValidationKAIConfig.Spec.QueueController.Webhooks.EnableValidation = ptr.To(false)

				objects, err := qc.DesiredState(ctx, fakeKubeClient, noValidationKAIConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				validatingWebhookConfigurations := test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)
				Expect(len(validatingWebhookConfigurations)).To(Equal(0))
				Expect(validatingWebhookConfigurations).To(BeNil())
			})

			It("the validating webhooks should keep labels from existing validating webhooks", func(ctx context.Context) {
				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				validatingWebhookConfigurations := test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)
				Expect(len(validatingWebhookConfigurations)).To(BeNumerically(">", 0))

				validatingWebhookConfiguration := validatingWebhookConfigurations[0]
				maps.Copy(validatingWebhookConfiguration.Labels, map[string]string{
					"foo": "bar",
				})
				Expect(fakeKubeClient.Create(ctx, validatingWebhookConfiguration)).To(Succeed())

				objects, err = qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				validatingWebhookConfigurations = test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)
				Expect(len(validatingWebhookConfigurations)).To(BeNumerically(">", 0))

				validatingWebhookConfiguration = validatingWebhookConfigurations[0]

				Expect(validatingWebhookConfiguration.Labels).To(HaveKeyWithValue("foo", "bar"))
			})
		})

		Context("PodDisruptionBudget", func() {
			It("includes PDB when HA and enabled with matching deployment selector", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled:        ptr.To(true),
					MaxUnavailable: ptr.To(int32(1)),
				}

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				pdbs := test_utils.FindTypesInObjects[*policyv1.PodDisruptionBudget](objects)
				Expect(pdbs).To(HaveLen(1))
				Expect(pdbs[0].Name).To(Equal(defaultResourceName))
				Expect(pdbs[0].Namespace).To(Equal(constants.DefaultKAINamespace))
				Expect(pdbs[0].Spec.MaxUnavailable).NotTo(BeNil())
				Expect(pdbs[0].Spec.MaxUnavailable.IntVal).To(Equal(int32(1)))
				Expect(pdbs[0].Spec.Selector.MatchLabels["app"]).To(Equal(defaultResourceName))

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				Expect(pdbs[0].Spec.Selector.MatchLabels["app"]).To(Equal((*deploymentT).Spec.Template.Labels["app"]))
			})

			It("uses custom maxUnavailable", func(ctx context.Context) {
				qc.BaseResourceName = defaultResourceName
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled:        ptr.To(true),
					MaxUnavailable: ptr.To(int32(2)),
				}

				objects, err := qc.podDisruptionBudgetForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(objects).To(HaveLen(1))

				pdb := objects[0].(*policyv1.PodDisruptionBudget)
				Expect(pdb.Spec.MaxUnavailable).NotTo(BeNil())
				Expect(pdb.Spec.MaxUnavailable.IntVal).To(Equal(int32(2)))
			})

			It("preserves resourceVersion from an existing PDB", func(ctx context.Context) {
				existing := &policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:            defaultResourceName,
						Namespace:       constants.DefaultKAINamespace,
						ResourceVersion: "42",
						Labels: map[string]string{
							"app": defaultResourceName,
						},
					},
				}
				fakeKubeClient = fake.NewClientBuilder().WithObjects(existing).Build()
				qc.BaseResourceName = defaultResourceName

				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled:        ptr.To(true),
					MaxUnavailable: ptr.To(int32(1)),
				}

				objects, err := qc.podDisruptionBudgetForKAIConfig(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(objects).To(HaveLen(1))

				pdb := objects[0].(*policyv1.PodDisruptionBudget)
				Expect(pdb.ResourceVersion).To(Equal("42"))
				Expect(pdb.Spec.MaxUnavailable).NotTo(BeNil())
				Expect(pdb.Spec.MaxUnavailable.IntVal).To(Equal(int32(1)))
			})

			It("omits PDB when HA but disabled", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled: ptr.To(false),
				}

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				for _, obj := range objects {
					Expect(obj).NotTo(BeAssignableToTypeOf(&policyv1.PodDisruptionBudget{}))
				}
			})

			It("omits PDB when single replica even if enabled", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(1))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled: ptr.To(true),
				}

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				for _, obj := range objects {
					Expect(obj).NotTo(BeAssignableToTypeOf(&policyv1.PodDisruptionBudget{}))
				}
			})

			It("omits PDB when PDB config is missing and defaults apply", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = nil
				kaiConfig.Spec.QueueController.Service.SetDefaultsWhereNeeded("")

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(kaiConfig.Spec.QueueController.Service.PodDisruptionBudget).NotTo(BeNil())
				Expect(*kaiConfig.Spec.QueueController.Service.PodDisruptionBudget.Enabled).To(BeFalse())

				for _, obj := range objects {
					Expect(obj).NotTo(BeAssignableToTypeOf(&policyv1.PodDisruptionBudget{}))
				}
			})

			It("includes PDB when validation webhooks are disabled", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Webhooks.EnableValidation = ptr.To(false)
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled:        ptr.To(true),
					MaxUnavailable: ptr.To(int32(1)),
				}

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				pdbs := test_utils.FindTypesInObjects[*policyv1.PodDisruptionBudget](objects)
				Expect(pdbs).To(HaveLen(1))
				Expect(test_utils.FindTypesInObjects[*v1.ValidatingWebhookConfiguration](objects)).To(BeEmpty())
			})

			It("returns empty desired state when queue controller is disabled", func(ctx context.Context) {
				kaiConfig.Spec.QueueController.Service.Enabled = ptr.To(false)
				kaiConfig.Spec.QueueController.Replicas = ptr.To(int32(2))
				kaiConfig.Spec.QueueController.Service.PodDisruptionBudget = &common.PodDisruptionBudget{
					Enabled: ptr.To(true),
				}

				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(objects).To(BeEmpty())
			})
		})
	})
})

func kaiConfigForQueueController() *kaiv1.Config {
	kaiConfig := &kaiv1.Config{}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Spec.QueueController.Service.Enabled = ptr.To(true)

	return kaiConfig
}
