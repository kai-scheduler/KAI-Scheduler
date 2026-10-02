// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	podgrouperconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeAllocatedNonPreemptibleSpecs() bool {
	return Describe("PodGroup allocatedNonPreemptible", Ordered, func() {
		var (
			testCtx   *testcontext.TestContext
			testQueue *v2.Queue
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("is cleared once the pod group becomes preemptible", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			job := rd.CreateBatchJobObject(testQueue, v1.ResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
			})
			job.Spec.Template.Labels[podgrouperconstants.PreemptibilityLabelKey] = string(v2alpha2.NonPreemptible)
			job, err := testCtx.KubeClientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			var pod v1.Pod
			Eventually(func(g Gomega) {
				pods := rd.GetJobPods(ctx, testCtx.KubeClientset, job)
				g.Expect(pods).To(HaveLen(1))
				pod = pods[0]
			}, time.Minute, time.Second).Should(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, &pod)
			podGroupName := SpecWaitForPGAnnotationOnPod(ctx, testCtx, &pod)

			podGroup := &v2alpha2.PodGroup{}
			podGroupKey := runtimeClient.ObjectKey{Namespace: pod.Namespace, Name: podGroupName}
			Eventually(func(g Gomega) {
				g.Expect(testCtx.ControllerClient.Get(ctx, podGroupKey, podGroup)).To(Succeed())
				g.Expect(podGroup.Status.ResourcesStatus.AllocatedNonPreemptible).To(HaveKey(v1.ResourceCPU))
			}, time.Minute, time.Second).Should(Succeed())

			// The podgrouper rebuilds the pod group's spec from the pod's label.
			Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(&pod), &pod)).To(Succeed())
			original := pod.DeepCopy()
			pod.Labels[podgrouperconstants.PreemptibilityLabelKey] = string(v2alpha2.Preemptible)
			Expect(testCtx.ControllerClient.Patch(ctx, &pod, runtimeClient.MergeFrom(original))).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testCtx.ControllerClient.Get(ctx, podGroupKey, podGroup)).To(Succeed())
				g.Expect(podGroup.Spec.Preemptibility).To(Equal(v2alpha2.Preemptible))
				g.Expect(podGroup.Status.ResourcesStatus.AllocatedNonPreemptible).To(BeEmpty())
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
}
