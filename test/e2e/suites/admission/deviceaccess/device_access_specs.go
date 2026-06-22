/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package deviceaccess

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

// webhookPropagationTimeout bounds how long we retry pod submissions after toggling the
// flag: SetBlockNvidiaVisibleDevices already waits for the admission Deployment rollout to
// complete, but there is a brief window where the rolled-out webhook is not yet enforcing
// the new config. Each retry uses a freshly-named pod, so admitted pods don't collide.
const webhookPropagationTimeout = time.Minute

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
				expectAdmitted(ctx, testCtx, "all")
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
					expectRejected(ctx, testCtx, value)
				},
				Entry("single index", "1"),
				Entry("multiple indexes", "1,2"),
				Entry("all", "all"),
			)

			DescribeTable("admits allowed NVIDIA_VISIBLE_DEVICES values",
				func(ctx context.Context, value string) {
					expectAdmitted(ctx, testCtx, value)
				},
				Entry("void", "void"),
				Entry("none", "none"),
			)

			It("injects NVIDIA_VISIBLE_DEVICES=void into non-GPU containers, exempting the GPU container", func(ctx context.Context) {
				Eventually(func(g Gomega) {
					pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1"),
						},
					})
					gpuContainerName := pod.Spec.Containers[0].Name
					pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
						Name:  "cpu-sidecar",
						Image: pod.Spec.Containers[0].Image,
					})

					created, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(visibleDevicesValue(created, "cpu-sidecar")).To(Equal("void"))
					g.Expect(visibleDevicesValue(created, gpuContainerName)).ToNot(Equal("void"))
				}).WithContext(ctx).WithTimeout(webhookPropagationTimeout).WithPolling(2 * time.Second).Should(Succeed())
			})
		})
	})
}

// expectRejected retries (with a fresh pod each time) until the admission webhook rejects a
// pod setting NVIDIA_VISIBLE_DEVICES to the given value. It only passes on a real rejection,
// so it tolerates webhook-propagation delay without masking a non-enforcing webhook.
func expectRejected(ctx context.Context, testCtx *testcontext.TestContext, value string) {
	Eventually(func() error {
		_, err := createPodWithVisibleDevices(ctx, testCtx, value)
		return err
	}).WithContext(ctx).WithTimeout(webhookPropagationTimeout).WithPolling(2*time.Second).
		Should(HaveOccurred(), "expected pod with NVIDIA_VISIBLE_DEVICES=%s to be rejected", value)
}

// expectAdmitted retries (with a fresh pod each time) until a pod setting NVIDIA_VISIBLE_DEVICES
// to the given value is admitted.
func expectAdmitted(ctx context.Context, testCtx *testcontext.TestContext, value string) {
	Eventually(func() error {
		_, err := createPodWithVisibleDevices(ctx, testCtx, value)
		return err
	}).WithContext(ctx).WithTimeout(webhookPropagationTimeout).WithPolling(2*time.Second).
		Should(Succeed(), "expected pod with NVIDIA_VISIBLE_DEVICES=%s to be admitted", value)
}

// createPodWithVisibleDevices builds a kai-scheduler pod (fresh random name) whose container
// requests a GPU and sets NVIDIA_VISIBLE_DEVICES to the given value, then submits it directly.
//
// The container must request a GPU: the mutating webhook runs before the validating one and
// injects NVIDIA_VISIBLE_DEVICES=void into non-GPU containers, which would neutralize a
// forbidden value before validation ever sees it. GPU-requesting containers are exempt from
// that mutation, so the validation path is actually exercised.
func createPodWithVisibleDevices(ctx context.Context, testCtx *testcontext.TestContext, value string) (*v1.Pod, error) {
	pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1"),
		},
	})
	pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, v1.EnvVar{
		Name:  constants.NvidiaVisibleDevices,
		Value: value,
	})
	return testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
}

// visibleDevicesValue returns the NVIDIA_VISIBLE_DEVICES env value of the named container ("" if unset).
func visibleDevicesValue(pod *v1.Pod, containerName string) string {
	for _, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, env := range container.Env {
			if env.Name == constants.NvidiaVisibleDevices {
				return env.Value
			}
		}
	}
	return ""
}
