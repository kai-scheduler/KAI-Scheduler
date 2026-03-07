// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_grouper

import (
	"context"
	"testing"

	"golang.org/x/exp/maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enginev1alpha1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	test_utils "github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/common/test_utils"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodGrouper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodGrouper operand Suite")
}

var _ = Describe("PodGrouper", func() {
	Describe("DesiredState", func() {
		var (
			fakeKubeClient client.Client
			pg             *PodGrouper
			engineConfig   *enginev1alpha1.Config
		)
		BeforeEach(func(ctx context.Context) {
			fakeKubeClient = fake.NewFakeClient()
			pg = &PodGrouper{}
			engineConfig = engineConfigForPodGrouper()
		})

		Context("Deployment", func() {
			It("should return a Deployment in the objects list", func(ctx context.Context) {
				objects, err := pg.DesiredState(ctx, fakeKubeClient, engineConfig)
				Expect(err).To(BeNil())
				Expect(len(objects)).To(BeNumerically(">", 1))

				deploymentT := test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment := *deploymentT
				Expect(deployment).NotTo(BeNil())
				Expect(deployment.Name).To(Equal(defaultResourceName))
			})

			It("the deployment should keep labels from existing deployment", func(ctx context.Context) {
				objects, err := pg.DesiredState(ctx, fakeKubeClient, engineConfig)
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

				objects, err = pg.DesiredState(ctx, fakeKubeClient, engineConfig)
				Expect(err).To(BeNil())

				deploymentT = test_utils.FindTypeInObjects[*appsv1.Deployment](objects)
				Expect(deploymentT).NotTo(BeNil())
				deployment = *deploymentT
				Expect(deployment.Labels).To(HaveKeyWithValue("foo", "bar"))
				Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("run", "ai"))
			})
		})
	})
})

func engineConfigForPodGrouper() *enginev1alpha1.Config {
	engineConfig := &enginev1alpha1.Config{}
	engineConfig.Spec.SetDefaultsWhereNeeded()
	engineConfig.Spec.PodGrouper.Service.Enabled = ptr.To(true)

	return engineConfig
}
