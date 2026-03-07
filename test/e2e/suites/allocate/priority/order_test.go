/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package priority

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const (
	priorityClassLabelName = "priorityClassName"
)

var _ = Describe("Order jobs allocation queue", Label(labels.Operated), Ordered, func() {
	var (
		testCtx      *testcontext.TestContext
		lowPriority  string
		highPriority string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
			Gpu:      resource.MustParse("2"),
			PodCount: 2,
		})
		parentQueue := queue.CreateQueueObject("parent-"+utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObjectWithGpuResource("test-queue-"+utils.GenerateRandomK8sName(10),
			v2.QueueResource{
				Quota:           1,
				OverQuotaWeight: 1,
				Limit:           1,
			}, parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		lowPriority = "low-" + utils.GenerateRandomK8sName(10)
		lowPriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
		_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowPriority, lowPriorityValue), metav1.CreateOptions{})
		Expect(err).To(Succeed())

		highPriority = "high-" + utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highPriority, lowPriorityValue+1), metav1.CreateOptions{})
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

	It("PriorityClass as a label", func(ctx context.Context) {
		configurations.DisableScheduler(ctx, testCtx)
		lowPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		})
		lowPriorityPod.Name = "low-priority-pod"
		lowPriorityPod.Labels[priorityClassLabelName] = lowPriority
		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPriorityPod)
		Expect(err).NotTo(HaveOccurred())

		highPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		})
		highPriorityPod.Name = "high-priority-pod"
		highPriorityPod.Labels[priorityClassLabelName] = highPriority
		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPriorityPod)
		Expect(err).NotTo(HaveOccurred())

		wait.WaitForPodGroupsToBeReady(ctx, testCtx.ControllerClient,
			queue.GetConnectedNamespaceToQueue(testCtx.Queues[0]), 2)

		configurations.EnableScheduler(ctx, testCtx)
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPriorityPod)
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPriorityPod)
	})

	It("PriorityClass in pod.spec", func(ctx context.Context) {
		configurations.DisableScheduler(ctx, testCtx)
		lowPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		})
		lowPriorityPod.Name = "low-priority-pod"
		lowPriorityPod.Spec.PriorityClassName = lowPriority
		_, err := rd.CreatePod(ctx, testCtx.KubeClientset, lowPriorityPod)
		Expect(err).NotTo(HaveOccurred())

		highPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		})
		highPriorityPod.Name = "high-priority-pod"
		highPriorityPod.Spec.PriorityClassName = highPriority
		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPriorityPod)
		Expect(err).NotTo(HaveOccurred())

		wait.WaitForPodGroupsToBeReady(ctx, testCtx.ControllerClient,
			queue.GetConnectedNamespaceToQueue(testCtx.Queues[0]), 2)

		configurations.EnableScheduler(ctx, testCtx)
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPriorityPod)
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, lowPriorityPod)
	})
})
