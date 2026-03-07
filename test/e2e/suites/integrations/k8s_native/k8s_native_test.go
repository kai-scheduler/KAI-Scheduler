/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package k8s_native

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

var _ = DescribeK8sNativeSpecs()

var _ = Describe("K8S Native object integrations", Ordered, func() {
	var testCtx *testcontext.TestContext

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	AfterAll(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
	})

	Context("Pods", func() {
		It("considers pod Overhead from runtimeclass", func(ctx context.Context) {
			runtimeClassName := "my-runtime-class-" + utils.GenerateRandomK8sName(5)
			limitedQueue := queue.CreateQueueObject("limited-"+utils.GenerateRandomK8sName(10), testCtx.Queues[1].Name)
			limitedQueue.Spec.Resources.CPU.Limit = 1
			testCtx.AddQueues(ctx, []*v2.Queue{limitedQueue})

			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, limitedQueue)).To(Succeed())
			}()

			pod := rd.CreatePodObject(limitedQueue, v1.ResourceRequirements{})
			pod.Spec.RuntimeClassName = &runtimeClassName

			runtimeClass := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeClassName,
				},
				Handler: "runc",
				Overhead: &nodev1.Overhead{
					PodFixed: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("2"),
					},
				},
			}

			Expect(testCtx.ControllerClient.Create(ctx, runtimeClass)).To(Succeed())

			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, runtimeClass)).To(Succeed())
			}()

			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			if err != nil {
				Expect(err).NotTo(HaveOccurred(), "Failed to create pod-job")
			}

			defer func() {
				Expect(testCtx.ControllerClient.Delete(ctx, pod)).To(Succeed())
			}()

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})
	})
})
