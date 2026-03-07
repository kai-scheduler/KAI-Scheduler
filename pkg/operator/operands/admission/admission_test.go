// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"testing"

	"golang.org/x/exp/maps"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	kaiv1admission "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/admission"
	kaiv1common "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common/test_utils"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAdmission(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admission operand Suite")
}

var _ = Describe("Admission", func() {
	var (
		fakeKubeClient client.Client
		a              *Admission
		kaiConfig      *kaiv1.Config
		ctx            context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		testScheme := scheme.Scheme
		utilruntime.Must(kaiv1.AddToScheme(testScheme))

		fakeKubeClient = fake.NewClientBuilder().WithScheme(testScheme).Build()
		a = &Admission{}
		kaiConfig = kaiConfigForAdmission()
	})

	Describe("DesiredState", func() {
		Context("when admission is not enabled", func() {
			It("should return no objects", func() {
				kaiConfig.Spec.Admission.Service.Enabled = ptr.To(false)
				objects, err := a.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(objects).To(BeEmpty())
			})
		})

		Context("when admission is enabled", func() {
			var objects []client.Object
			var err error

			BeforeEach(func() {
				objects, err = a.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))
			})

			It("should use the same secret in deployment and both webhooks", func() {
				secret := getSecret(objects)
				deployment := getDeployment(objects)
				mutatingWebhook := getMutatingWebhook(objects)
				validatingWebhook := getValidatingWebhook(objects)

				Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
				Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).To(Equal(secret.Name))

				Expect(mutatingWebhook.Webhooks).To(HaveLen(1))
				Expect(mutatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(secret.Data[certKey]))

				Expect(validatingWebhook.Webhooks).To(HaveLen(1))
				Expect(validatingWebhook.Webhooks[0].ClientConfig.CABundle).To(Equal(secret.Data[certKey]))

				Expect(mutatingWebhook.Webhooks[0].ClientConfig.Service.Name).To(Equal(defaultResourceName))
				Expect(validatingWebhook.Webhooks[0].ClientConfig.Service.Name).To(Equal(defaultResourceName))
			})

			It("should preserve existing deployment labels", func() {
				existingDeployment := getDeployment(objects)
				maps.Copy(existingDeployment.Labels, map[string]string{"foo": "bar"})
				maps.Copy(existingDeployment.Spec.Template.Labels, map[string]string{"kai": "scheduler"})
				Expect(fakeKubeClient.Create(ctx, existingDeployment)).To(Succeed())

				updatedObjects, err := a.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				updatedDeployment := getDeployment(updatedObjects)
				Expect(updatedDeployment.Labels).To(HaveKeyWithValue("foo", "bar"))
				Expect(updatedDeployment.Spec.Template.Labels).To(HaveKeyWithValue("kai", "scheduler"))
			})

			It("should configure service with correct selector", func() {
				service := getService(objects)
				Expect(service.Spec.Selector).To(HaveKeyWithValue("app", defaultResourceName))
			})
		})
	})
})

func getDeployment(objects []client.Object) *appsv1.Deployment {
	deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
	Expect(deploymentT).NotTo(BeNil())
	return *deploymentT
}

func getService(objects []client.Object) *v1.Service {
	serviceT := test_utils.FindTypeInObjects[*v1.Service](objects)
	Expect(serviceT).NotTo(BeNil())
	return *serviceT
}

func getMutatingWebhook(objects []client.Object) *admissionv1.MutatingWebhookConfiguration {
	webhookT := test_utils.FindTypeInObjects[*admissionv1.MutatingWebhookConfiguration](objects)
	Expect(webhookT).NotTo(BeNil())
	return *webhookT
}

func getValidatingWebhook(objects []client.Object) *admissionv1.ValidatingWebhookConfiguration {
	webhookT := test_utils.FindTypeInObjects[*admissionv1.ValidatingWebhookConfiguration](objects)
	Expect(webhookT).NotTo(BeNil())
	return *webhookT
}

func getSecret(objects []client.Object) *v1.Secret {
	secretT := test_utils.FindTypeInObjects[*v1.Secret](objects)
	Expect(secretT).NotTo(BeNil())
	return *secretT
}

func kaiConfigForAdmission() *kaiv1.Config {
	kaiConfig := &kaiv1.Config{}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Spec.Admission.Service.Enabled = ptr.To(true)
	kaiConfig.Spec.Admission.Replicas = ptr.To(int32(1))
	kaiConfig.Spec.Admission.Service.Resources = &kaiv1common.Resources{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("50m"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("100m"),
			v1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
	kaiConfig.Spec.Admission.Webhook = &kaiv1admission.Webhook{
		Port:        ptr.To(443),
		TargetPort:  ptr.To(9443),
		ProbePort:   ptr.To(8081),
		MetricsPort: ptr.To(8080),
	}
	kaiConfig.Spec.Admission.Service.Image = &kaiv1common.Image{
		Name:       ptr.To("admission"),
		Repository: ptr.To("registry/local/kai-scheduler"),
		Tag:        ptr.To("latest"),
		PullPolicy: ptr.To(v1.PullIfNotPresent),
	}
	kaiConfig.Spec.Global.SchedulerName = ptr.To("kai-scheduler")
	kaiConfig.Spec.Global.QueueLabelKey = ptr.To("kai.scheduler/queue")

	return kaiConfig
}
