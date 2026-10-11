// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

func DescribeReclaimTerminatingVictimsSpecs() bool {
	return Describe("Reclaim while victims terminate", Ordered, ContinueOnFailure, func() {
		var (
			testCtx        *testcontext.TestContext
			victimQueue    *v2.Queue
			reclaimerQueue *v2.Queue
			lowPriority    string
			highPriority   string
			tierLabels     map[string]string
			victims        map[string]*v1.Pod
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{Gpu: resource.MustParse("8"), PodCount: 2},
				{Gpu: resource.MustParse("8"), PodCount: 2},
				{Gpu: resource.MustParse("8"), PodCount: 2},
			})

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			victimQueue = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
				v2.QueueResource{Quota: 0, OverQuotaWeight: 1, Limit: -1}, parentQueue.Name)
			reclaimerQueue = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
				v2.QueueResource{Quota: 12, OverQuotaWeight: 1, Limit: -1}, parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{parentQueue, victimQueue, reclaimerQueue})

			lowPriority, highPriority = utils.GenerateRandomK8sName(10), utils.GenerateRandomK8sName(10)
			lowPriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
			for name, value := range map[string]int{lowPriority: lowPriorityValue, highPriority: lowPriorityValue + 1} {
				_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
					Create(ctx, rd.CreatePriorityClass(name, value), metav1.CreateOptions{})
				Expect(err).To(Succeed())
			}
		})

		BeforeEach(func(ctx context.Context) {
			tierLabels = map[string]string{"tier": utils.GenerateRandomK8sName(10)}
			// A 2-GPU pod fits beside a 6-GPU victim but for its anti-affinity.
			victims = fillers.FillGPUNodesWithSlowTerminatingPods(ctx, testCtx, victimQueue, tierLabels, lowPriority, 6)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		AfterAll(func(ctx context.Context) {
			Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		createReclaimer := func(ctx context.Context, pod *v1.Pod, priorityClass string) *v1.Pod {
			pod.Spec.PriorityClassName = priorityClass
			return fillers.CreatePodRepellingVictims(ctx, testCtx, pod, tierLabels,
				queue.GetConnectedNamespaceToQueue(victimQueue), queue.GetConnectedNamespaceToQueue(reclaimerQueue))
		}

		It("Evicts one pod for a job anti-affine to it, and no other while it terminates", func(ctx context.Context) {
			reclaimer := createReclaimer(ctx, rd.CreatePodObject(reclaimerQueue, fillers.GPURequirements(2)), lowPriority)
			fillers.ExpectEvictionsUntilScheduled(ctx, testCtx, victims, 1, reclaimer)
		})

		It("Reclaims for another job of the queue while the first job's victim terminates", func(ctx context.Context) {
			// The higher priority keeps the first job ahead for the node its victim frees.
			first := createReclaimer(ctx, rd.CreatePodObject(reclaimerQueue, fillers.GPURequirements(2)), highPriority)
			evictedNodes := fillers.EvictedNodes(ctx, testCtx, victims)
			Eventually(evictedNodes).WithTimeout(time.Minute).WithPolling(time.Second).Should(HaveLen(1))

			second := createReclaimer(ctx, rd.CreatePodObject(reclaimerQueue, fillers.GPURequirements(2)), lowPriority)
			Eventually(evictedNodes).WithTimeout(fillers.VictimTerminationGracePeriod/2).WithPolling(time.Second).
				Should(HaveLen(2), "the second job does not wait for the first job's victim to terminate")
			fillers.ExpectEvictionsUntilScheduled(ctx, testCtx, victims, 2, first, second)
		})

		It("Evicts one pod per gang member, and no other while they terminate", func(ctx context.Context) {
			podGroup := pod_group.Create(queue.GetConnectedNamespaceToQueue(reclaimerQueue),
				utils.GenerateRandomK8sName(10), reclaimerQueue.Name)
			podGroup.Spec.MinMember = ptr.To(int32(2))
			_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(podGroup.Namespace).
				Create(ctx, podGroup, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			var members []*v1.Pod
			for range 2 {
				// A 6-GPU member needs a node of its own.
				member := rd.CreatePodWithPodGroupReference(reclaimerQueue, podGroup.Name, fillers.GPURequirements(6))
				members = append(members, createReclaimer(ctx, member, lowPriority))
			}
			fillers.ExpectEvictionsUntilScheduled(ctx, testCtx, victims, 2, members...)
		})
	})
}
