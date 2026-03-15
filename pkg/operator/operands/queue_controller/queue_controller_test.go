// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_controller

import (
	"context"
	"testing"

	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"golang.org/x/exp/maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
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
				objects, err := qc.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				Expect(deployment).NotTo(BeNil())
				Expect(deployment.Name).To(Equal(defaultResourceName))
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
	})
})

func kaiConfigForQueueController() *kaiv1.Config {
	kaiConfig := &kaiv1.Config{}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Spec.QueueController.Service.Enabled = ptr.To(true)

	return kaiConfig
}
