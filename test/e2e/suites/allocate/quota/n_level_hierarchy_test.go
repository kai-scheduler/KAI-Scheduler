/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package quota

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = Describe("N-Level Queue Hierarchy", Ordered, func() {
	Context("Single level queue hierarchy", func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Cpu:      resource.MustParse("100m"),
				PodCount: 1,
			})

			// Single root queue with no parent
			rootQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			rootQueue.Spec.Resources.CPU.Quota = 500
			rootQueue.Spec.Resources.CPU.Limit = 500

			testCtx.InitQueues([]*v2.Queue{rootQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should allocate job in single level queue", func(ctx context.Context) {
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			})
			pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			updatedPod, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			Expect(err).To(Succeed())

			podGroup, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(updatedPod.Namespace).Get(ctx, updatedPod.Annotations[pod_group.PodGroupNameAnnotation], metav1.GetOptions{})
			Expect(err).To(Succeed())
			for _, condition := range podGroup.Status.SchedulingConditions {
				Expect(condition.Type).NotTo(Equal(v2alpha2.UnschedulableOnNodePool), "PodGroup should not have UnschedulableOnNodePool schedulingCondition")
			}
		})
	})

	Context("Three level queue hierarchy", func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Cpu:      resource.MustParse("200m"),
				PodCount: 2,
			})

			// Level 1: Organization (root)
			orgQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			orgQueue.Spec.Resources.CPU.Quota = 1000
			orgQueue.Spec.Resources.CPU.Limit = 1000

			// Level 2: Department
			deptQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), orgQueue.Name)
			deptQueue.Spec.Resources.CPU.Quota = 500
			deptQueue.Spec.Resources.CPU.Limit = 500

			// Level 3: Team
			teamQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), deptQueue.Name)
			teamQueue.Spec.Resources.CPU.Quota = 200
			teamQueue.Spec.Resources.CPU.Limit = 200

			testCtx.InitQueues([]*v2.Queue{orgQueue, deptQueue, teamQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should allocate job at deepest level (team)", func(ctx context.Context) {
			// teamQueue is the 3rd queue (index 2)
			pod := rd.CreatePodObject(testCtx.Queues[2], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			})
			pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})

	Context("Four level queue hierarchy", func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
				Cpu:      resource.MustParse("100m"),
				PodCount: 1,
			})

			// Level 1: Company (root)
			companyQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			companyQueue.Spec.Resources.CPU.Quota = 2000
			companyQueue.Spec.Resources.CPU.Limit = 2000

			// Level 2: Division
			divisionQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), companyQueue.Name)
			divisionQueue.Spec.Resources.CPU.Quota = 1000
			divisionQueue.Spec.Resources.CPU.Limit = 1000

			// Level 3: Department
			deptQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), divisionQueue.Name)
			deptQueue.Spec.Resources.CPU.Quota = 500
			deptQueue.Spec.Resources.CPU.Limit = 500

			// Level 4: Project
			projectQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), deptQueue.Name)
			projectQueue.Spec.Resources.CPU.Quota = 200
			projectQueue.Spec.Resources.CPU.Limit = 200

			testCtx.InitQueues([]*v2.Queue{companyQueue, divisionQueue, deptQueue, projectQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should allocate job at deepest level (project)", func(ctx context.Context) {
			// projectQueue is the 4th queue (index 3)
			pod := rd.CreatePodObject(testCtx.Queues[3], v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			})
			pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
})
