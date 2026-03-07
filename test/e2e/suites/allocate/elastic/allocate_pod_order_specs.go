/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package elastic

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const (
	specTaskOrderLabelKey = "kai.scheduler/task-priority"
)

func DescribeAllocatePodOrderSpecs() bool {
	return Describe("Elastic allocation pods order tests", Ordered, func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			childQueue.Spec.Resources.CPU.Quota = 500
			childQueue.Spec.Resources.CPU.Limit = 500
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("500m"),
					PodCount: 2,
				},
			})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("Check ordered pods with elastic partial allocation", func(ctx context.Context) {
			pgName := utils.GenerateRandomK8sName(10)
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			pod0 := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			pod0.Labels[specTaskOrderLabelKey] = "1"
			pod0, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod0)
			Expect(err).To(Succeed())

			pod1 := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			pod1.Labels[specTaskOrderLabelKey] = "3"
			pod1, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			pod2 := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			pod2.Labels[specTaskOrderLabelKey] = "2"
			pod2, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod2)
			Expect(err).To(Succeed())

			_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				pod_group.Create(namespace, pgName, testCtx.Queues[0].Name),
				metav1.CreateOptions{})
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod2)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod0)
		})

		It("Check ordered pods with elastic partial allocation - with pods missing label", func(ctx context.Context) {
			pgName := utils.GenerateRandomK8sName(10)
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			pod0 := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("300m"),
				},
			})
			pod0, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod0)
			Expect(err).To(Succeed())

			pod1 := rd.CreatePodWithPodGroupReference(testCtx.Queues[0], pgName, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("300m"),
				},
			})
			pod1.Labels[specTaskOrderLabelKey] = "3"
			pod1, err = rd.CreatePod(ctx, testCtx.KubeClientset, pod1)
			Expect(err).To(Succeed())

			_, err = testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				pod_group.Create(namespace, pgName, testCtx.Queues[0].Name),
				metav1.CreateOptions{})
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod1)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod0)
		})
	})
}
