/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

// DescribePreemptSemiPreemptibleSpecs proves the two-step semi-preemptible lifecycle: a job first expands
// beyond its minimum (elastic burst, over-quota) when capacity is free, then scales down to exactly its
// core when a higher-priority job needs the elastic capacity.
func DescribePreemptSemiPreemptibleSpecs() bool {
	return Describe("Semi-Preemptible Elastic Lifecycle", Ordered, func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Cpu:      resource.MustParse("600m"),
					PodCount: 8,
				},
			})
		})

		AfterAll(func(ctx context.Context) {
			err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
			Expect(err).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		It("subgroup shape: bursts over-quota then scales down to core subgroups on higher-priority arrival", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			// low queue quota sized for the 2 core subgroups only (2 * 100m).
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 200
			lowQueue.Spec.Resources.CPU.Limit = 600
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 200
			highQueue.Spec.Resources.CPU.Limit = 600
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			// 4 fully-gang leaf subgroups, minSubGroup=2 → 2 core, 2 elastic.
			_, h := pod_group.CreateWithHierarchy(ctx, testCtx.KubeClientset, testCtx.KubeAiSchedClientset,
				utils.GenerateRandomK8sName(10), lowQueue, ptr.To[int32](2),
				flatLeaves("sg", 4, 1), nil, v2alpha2.SemiPreemptible, cpuPerPod)

			// Step 1 — expands beyond its minimum: all 4 subgroups scheduled (elastic tier over-quota).
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, 4)

			// Step 2 — a higher-priority job arrives and needs the elastic capacity.
			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// The semi-preemptible job scales down to exactly its 2 core subgroups; the core keeps running.
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, h.AllPods, 2)
		})

		It("pod shape: bursts over-quota then scales down to core pods on higher-priority arrival", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			lowQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			lowQueue.Spec.Resources.CPU.Quota = 200 // sized for 2 core pods
			lowQueue.Spec.Resources.CPU.Limit = 600
			highQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			highQueue.Spec.Resources.CPU.Quota = 200
			highQueue.Spec.Resources.CPU.Limit = 600
			testCtx.InitQueues([]*v2.Queue{lowQueue, highQueue, parentQueue})

			lowNamespace := queue.GetConnectedNamespaceToQueue(lowQueue)
			cpuPerPod := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("100m"),
				},
			}

			// Single elastic PodSet: minMember=2 (core), 4 pods total (2 elastic).
			pods := createSemiPreemptiblePodGroup(ctx, testCtx, lowQueue, ptr.To[int32](2), 4, cpuPerPod)

			// Step 1 — bursts to all 4 pods (elastic over-quota).
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, pods, 4)

			// Step 2 — higher-priority job needs the elastic capacity.
			highPod := rd.CreatePodObject(highQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("200m"),
				},
			})
			highPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, highPod)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPod)

			// Scales down to exactly its 2 core pods; the core keeps running.
			wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, lowNamespace, pods, 2)
		})
	})
}

// createSemiPreemptiblePodGroup creates a single-PodSet semi-preemptible PodGroup with the given core
// minMember and total pod count, returning the created pods.
func createSemiPreemptiblePodGroup(
	ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue, minMember *int32, numPods int,
	requirements v1.ResourceRequirements,
) []*v1.Pod {
	namespace := queue.GetConnectedNamespaceToQueue(q)
	podGroupName := utils.GenerateRandomK8sName(10)

	podGroup := pod_group.Create(namespace, podGroupName, q.Name)
	podGroup.Spec.MinMember = minMember
	podGroup.Spec.Preemptibility = v2alpha2.SemiPreemptible
	_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx, podGroup, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	var pods []*v1.Pod
	for i := 0; i < numPods; i++ {
		pod := rd.CreatePodWithPodGroupReference(q, podGroupName, requirements)
		pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
		Expect(err).To(Succeed())
		pods = append(pods, pod)
	}
	return pods
}
