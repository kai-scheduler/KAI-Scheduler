/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package deviceaccess

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

func DescribeDeviceAccessSpecs() bool {
	return Describe("Device access admission validation", Ordered, func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			// Restore the default (disabled) state regardless of which context ran last.
			Expect(feature_flags.SetBlockNvidiaVisibleDevices(ctx, testCtx, false)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		Context("when device access validation is disabled (default)", Ordered, func() {
			BeforeAll(func(ctx context.Context) {
				Expect(feature_flags.SetBlockNvidiaVisibleDevices(ctx, testCtx, false)).To(Succeed())
			})

			It("admits a pod overriding NVIDIA_VISIBLE_DEVICES=all", func(ctx context.Context) {
				_, err := createPodWithVisibleDevices(ctx, testCtx, "all")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when device access validation is enabled", Ordered, func() {
			BeforeAll(func(ctx context.Context) {
				Expect(feature_flags.SetBlockNvidiaVisibleDevices(ctx, testCtx, true)).To(Succeed())
			})

			AfterAll(func(ctx context.Context) {
				Expect(feature_flags.SetBlockNvidiaVisibleDevices(ctx, testCtx, false)).To(Succeed())
			})

			DescribeTable("rejects forbidden NVIDIA_VISIBLE_DEVICES values",
				func(ctx context.Context, value string) {
					_, err := createPodWithVisibleDevices(ctx, testCtx, value)
					Expect(err).To(HaveOccurred())
				},
				Entry("single index", "1"),
				Entry("multiple indexes", "1,2"),
				Entry("all", "all"),
			)

			DescribeTable("admits allowed NVIDIA_VISIBLE_DEVICES values",
				func(ctx context.Context, value string) {
					_, err := createPodWithVisibleDevices(ctx, testCtx, value)
					Expect(err).ToNot(HaveOccurred())
				},
				Entry("void", "void"),
				Entry("none", "none"),
			)
		})
	})
}

// createPodWithVisibleDevices builds a kai-scheduler pod that sets NVIDIA_VISIBLE_DEVICES to
// the given value and submits it directly (single attempt) so admission rejections surface
// immediately instead of being retried.
func createPodWithVisibleDevices(ctx context.Context, testCtx *testcontext.TestContext, value string) (*v1.Pod, error) {
	pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, v1.EnvVar{
		Name:  constants.NvidiaVisibleDevices,
		Value: value,
	})
	return testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
}
