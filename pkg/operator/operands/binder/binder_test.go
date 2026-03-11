// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binder

import (
	"context"
	"testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"golang.org/x/exp/maps"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/NVIDIA/KAI-scheduler/pkg/operator/operands/common/test_utils"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBinder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Binder operand Suite")
}

var _ = Describe("Binder", func() {
	Describe("DesiredState", func() {
		var (
			fakeKubeClient client.Client
			b              *Binder
			kaiConfig      *kaiv1.Config
		)
		BeforeEach(func(ctx context.Context) {
			testScheme := scheme.Scheme
			utilruntime.Must(nvidiav1.AddToScheme(testScheme))
			fakeClientBuilder := fake.NewClientBuilder()
			fakeClientBuilder.WithScheme(testScheme)

			fakeKubeClient = fake.NewFakeClient()
			b = &Binder{}
			kaiConfig = kaiConfigForBinder()
		})

		Context("Not Enabled", func() {
			It("should return no objects", func(ctx context.Context) {
				kaiConfig.Spec.Binder.Service.Enabled = ptr.To(false)
				objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeZero())
			})
		})

		Context("Deployment", func() {
			It("should return a Deployment in the objects list", func(ctx context.Context) {
				objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				Expect(deployment).NotTo(BeNil())
				Expect(deployment.Name).To(Equal(defaultResourceName))
			})

			It("the deployment should keep labels from existing deployment", func(ctx context.Context) {
				objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				maps.Copy(deployment.Labels, map[string]string{
					"foo": "bar",
				})
				maps.Copy(deployment.Spec.Template.Labels, map[string]string{
					"kai": "scheduler",
				})
				Expect(fakeKubeClient.Create(ctx, deployment)).To(Succeed())

				objects, err = b.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				deploymentT = test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment = *deploymentT
				Expect(deployment.Labels).To(HaveKeyWithValue("foo", "bar"))
				Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("kai", "scheduler"))
			})

			Context("CDI Detection", func() {
				var (
					clusterPolicy *nvidiav1.ClusterPolicy
				)
				BeforeEach(func() {
					clusterPolicy = &nvidiav1.ClusterPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test",
						},
						Spec: nvidiav1.ClusterPolicySpec{
							CDI: nvidiav1.CDIConfigSpec{
								Enabled: ptr.To(true),
								Default: ptr.To(true),
							},
						},
					}
				})

				It("sets CDI flag if set in cluser policy", func(ctx context.Context) {
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())
					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=true"))
				})

				It("sets CDI flag to false if not set by default cluser policy", func(ctx context.Context) {
					clusterPolicy.Spec.CDI.Default = ptr.To(false)
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())
					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=false"))
				})

				It("detects CDI state with GPU Operator >= v25.10.0", func(ctx context.Context) {
					clusterPolicy.Labels = map[string]string{
						versionLabelName: "v25.10.1",
					}
					clusterPolicy.Spec.CDI.Default = ptr.To(false)
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())

					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=true"))
				})

				It("detects CDI state with GPU Operator < v25.10.0", func(ctx context.Context) {
					clusterPolicy.Labels = map[string]string{
						versionLabelName: "v24.8.2",
					}
					clusterPolicy.Spec.CDI.Default = ptr.To(false)
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())

					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=false"))
				})

				It("uses explicit CDIEnabled=true from config, ignoring ClusterPolicy", func(ctx context.Context) {
					clusterPolicy.Spec.CDI.Default = ptr.To(false)
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())

					kaiConfig.Spec.Binder.CDIEnabled = ptr.To(true)

					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=true"))
				})

				It("uses explicit CDIEnabled=false from config, ignoring ClusterPolicy", func(ctx context.Context) {
					clusterPolicy.Spec.CDI.Enabled = ptr.To(true)
					clusterPolicy.Spec.CDI.Default = ptr.To(true)
					Expect(fakeKubeClient.Create(ctx, clusterPolicy)).To(Succeed())

					kaiConfig.Spec.Binder.CDIEnabled = ptr.To(false)

					objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
					Expect(err).To(BeNil())

					deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
					Expect(deploymentT).NotTo(BeNil())
					Expect((*deploymentT).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cdi-enabled=false"))
				})
			})
		})

		Context("Reservation Service Account", func() {
			It("will not remove current image pull secrets", func(ctx context.Context) {
				kaiConfig.Spec.Global.ImagePullSecrets = []string{"test-secret"}

				reservationSA := &v1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: *kaiConfig.Spec.Binder.ResourceReservation.Namespace,
						Name:      *kaiConfig.Spec.Binder.ResourceReservation.ServiceAccountName,
					},
					ImagePullSecrets: []v1.LocalObjectReference{
						{Name: "existing"},
					},
				}
				Expect(fakeKubeClient.Create(ctx, reservationSA)).To(Succeed())
				objects, err := b.DesiredState(ctx, fakeKubeClient, kaiConfig)
				Expect(err).To(BeNil())

				var newReservationSA *v1.ServiceAccount
				for _, obj := range objects {
					sa, ok := obj.(*v1.ServiceAccount)
					if ok && sa.Name == reservationSA.Name {
						newReservationSA = sa
					}
				}

				Expect(newReservationSA).NotTo(BeNil())
				Expect(newReservationSA.ImagePullSecrets).To(HaveLen(2))
				Expect(newReservationSA.ImagePullSecrets).To(ContainElement(v1.LocalObjectReference{Name: "existing"}))
				Expect(newReservationSA.ImagePullSecrets).To(ContainElement(v1.LocalObjectReference{Name: "test-secret"}))
			})
		})
	})
})

func kaiConfigForBinder() *kaiv1.Config {
	kaiConfig := &kaiv1.Config{}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Spec.Binder.Service.Enabled = ptr.To(true)

	return kaiConfig
}
