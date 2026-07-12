/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const preemptionDelayLabelKey = "kai.scheduler/preemption-delay"

func DescribePreemptDelaySpecs() bool {
	return Describe("Preemption Delay", Ordered, func() {
		var (
			testCtx           *testcontext.TestContext
			lowPriorityClass  string
			highPriorityClass string
			preemptionDelay   = 90 * time.Second
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Cpu:      resource.MustParse("500m"),
				PodCount: 1,
			})

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testQueue.Spec.Resources.CPU.Quota = 500
			testQueue.Spec.Resources.CPU.Limit = 500
			testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})

			lowPriorityClass = utils.GenerateRandomK8sName(10)
			lowPriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
			_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
				Create(ctx, rd.CreatePriorityClass(lowPriorityClass, lowPriorityValue),
					metav1.CreateOptions{})
			Expect(err).To(Succeed())

			highPriorityClass = utils.GenerateRandomK8sName(10)
			_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
				Create(ctx, rd.CreatePriorityClass(highPriorityClass, lowPriorityValue+1),
					metav1.CreateOptions{})
			Expect(err).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("delayed preemptor does not preempt within its window, preempts after", func(ctx context.Context) {
			lowPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			lowPod.Spec.PriorityClassName = lowPriorityClass
			_, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, lowPod)

			highPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("500m"),
				},
			})
			highPod.Spec.PriorityClassName = highPriorityClass
			highPod.Labels[preemptionDelayLabelKey] = preemptionDelay.String()
			_, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())

			// Within the window the high-priority pod must not trigger preemption.
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, highPod)
			Consistently(func(g Gomega) {
				pod, err := testCtx.KubeClientset.CoreV1().Pods(lowPod.Namespace).
					Get(ctx, lowPod.Name, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				g.Expect(pod.DeletionTimestamp).To(BeNil())
			}).WithTimeout(preemptionDelay / 2).WithPolling(5 * time.Second).Should(Succeed())

			// After the window expires, preemption proceeds and the delayed pod schedules.
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)
		})
	})
}
