// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"context"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeSteadyFairShareReclaimSpecs() bool {
	return Describe("Steady fair-share reclaim", Ordered, func() {
		var (
			testCtx        *testcontext.TestContext
			victimQueue    *v2.Queue
			reclaimerQueue *v2.Queue
			priorityClass  string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			// Weights of 1 and 9 put the victim queue far above its steady fair share and the reclaimer far
			// below its own, whatever the cluster's size and other queues.
			victimQueue = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
				v2.QueueResource{Quota: 0, OverQuotaWeight: 1, Limit: -1}, parentQueue.Name)
			reclaimerQueue = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
				v2.QueueResource{Quota: 0, OverQuotaWeight: 9, Limit: -1}, parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{parentQueue, victimQueue, reclaimerQueue})

			var err error
			priorityClass, err = rd.CreatePreemptiblePriorityClass(ctx, testCtx.KubeClientset)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.SetSteadyFairShareReclaim(ctx, testCtx, nil)).To(Succeed())
			Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		It("Reclaims for a queue below its steady fair share when the free GPUs are fragmented", func(ctx context.Context) {
			nodesIdleResources, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
			Expect(err).To(Succeed())
			var gpuNodes []string
			for nodeName, idle := range nodesIdleResources {
				if idle.Gpu.Value() >= 3 {
					gpuNodes = append(gpuNodes, nodeName)
				}
			}
			if len(gpuNodes) < 2 {
				Skip("needs at least two nodes with at least 3 idle GPUs")
			}
			sort.Strings(gpuNodes)

			// Fill every node but one GPU, the reclaimer queue on the first node and the victim queue on
			// the others, so that a two-GPU pod fits nowhere and no job can move to make room for it.
			victimPodUIDs := map[*batchv1.Job]types.UID{}
			for i, nodeName := range gpuNodes {
				fillerQueue := victimQueue
				if i == 0 {
					fillerQueue = reclaimerQueue
				}
				gpus := resource.NewQuantity(nodesIdleResources[nodeName].Gpu.Value()-1, resource.DecimalSI)
				jobs, pods, err := fillers.FillAllNodesWithJobs(ctx, testCtx, fillerQueue, v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{constants.NvidiaGpuResource: *gpus},
				}, nil, nil, priorityClass, nodeName)
				Expect(err).To(Succeed())
				Expect(jobs).To(HaveLen(1))
				if i > 0 {
					victimPodUIDs[jobs[0]] = pods[0].UID
				}
			}

			pendingPod := rd.CreatePodObject(reclaimerQueue, v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{constants.NvidiaGpuResource: resource.MustParse("2")},
			})
			pendingPod.Spec.PriorityClassName = priorityClass
			pendingPod, err = rd.CreatePod(ctx, testCtx.KubeClientset, pendingPod)
			Expect(err).To(Succeed())
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pendingPod)

			replacedVictims := func() int {
				replaced := 0
				for job, uid := range victimPodUIDs {
					for _, pod := range rd.GetJobPods(ctx, testCtx.KubeClientset, job) {
						if pod.UID != uid {
							replaced++
						}
					}
				}
				return replaced
			}

			// Without steady fair-share reclaim every queue is within its fair share, capped by demand.
			Consistently(func(g Gomega) {
				pod := &v1.Pod{}
				g.Expect(testCtx.ControllerClient.Get(ctx, types.NamespacedName{
					Namespace: pendingPod.Namespace, Name: pendingPod.Name}, pod)).To(Succeed())
				g.Expect(pod.Spec.NodeName).To(BeEmpty())
				g.Expect(replacedVictims()).To(BeZero())
			}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

			Expect(feature_flags.SetSteadyFairShareReclaim(ctx, testCtx, ptr.To(true))).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pendingPod)
			Eventually(replacedVictims).WithTimeout(time.Minute).WithPolling(5 * time.Second).Should(Equal(1))
		})
	})
}
