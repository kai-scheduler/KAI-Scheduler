/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package timeaware

import (
	"context"
	"fmt"
	"time"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Time to wait for Prometheus to have non-zero samples for queue usage
	prometheusUsageTimeout = 2 * time.Minute
	// Time to wait for fairness to kick in (includes usage fetch interval)
	fairnessTimeout = 2 * time.Minute
)

var _ = Describe("Time Aware Fairness", Label("timeaware", "nightly"), Ordered, func() {
	BeforeAll(func(ctx context.Context) {
		// Ensure we have at least 1 GPU for the test
		capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
			{
				Gpu:      resource.MustParse("1"),
				PodCount: 2,
			},
		})
	})

	AfterEach(func(ctx context.Context) {
		testCtx.ClusterCleanup(ctx)
		Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
	})

	It("should schedule fairly based on historical usage", func(ctx context.Context) {
		By("Creating a department and two queues with asymmetric weights")
		// Get a fresh test context with the current context to avoid stale context from BeforeSuite
		testCtx = testcontext.GetConnectivity(ctx, Default)

		// Queue-a: very high weight (10000) → ~99.99% base fair share
		// Queue-b: low weight (1) → ~0.01% base fair share
		// Without time-aware (kValue=0): queue-b's tiny fair share may not justify reclaim
		// With time-aware (kValue=10000): queue-a's usage history shifts fair share dramatically
		queueA, queueB := createQueuesForTimeAwareFairness()
		testCtx.InitQueues([]*v2.Queue{queueA, queueB})

		By("Filling ALL cluster GPUs with queue-a pod-group pods to ensure resource contention")
		idleByNode, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
		Expect(err).To(Succeed())
		idleGPUs := int64(0)
		for _, rl := range idleByNode {
			idleGPUs += rl.Gpu.Value()
		}
		Expect(idleGPUs).To(BeNumerically(">", 0), "Cluster must have at least 1 idle GPU for the test")

		resources := v1.ResourceRequirements{
			Requests: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
			Limits: v1.ResourceList{
				constants.NvidiaGpuResource: resource.MustParse("1"),
			},
		}
		_, queueAPods := pod_group.CreateWithPods(
			ctx,
			testCtx.KubeClientset,
			testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10),
			queueA,
			int(idleGPUs),
			nil,
			v2alpha2.Preemptible,
			resources,
		)
		namespace := queue.GetConnectedNamespaceToQueue(queueA)
		wait.ForAtLeastNPodsScheduled(ctx, testCtx.ControllerClient, namespace, queueAPods, 1)

		By("Waiting for queue-controller to reflect full queue-a allocation in Queue status")
		Eventually(func(g Gomega) {
			updatedQueue, qErr := testCtx.KubeAiSchedClientset.SchedulingV2().Queues("").Get(ctx, queueA.Name, metav1.GetOptions{})
			g.Expect(qErr).NotTo(HaveOccurred())
			allocated := updatedQueue.Status.Allocated[constants.NvidiaGpuResource]
			g.Expect(allocated.Value()).To(BeNumerically(">=", idleGPUs), "Expected Queue status allocated GPUs to reach full cluster usage")
		}, prometheusUsageTimeout, 5*time.Second).Should(Succeed())

		By("Waiting for Prometheus to observe full queue-a GPU usage")
		// If Prometheus doesn't have series for queue-a, time-aware fairness can't adjust weights.
		// We use the apiserver proxy to query the managed Prometheus instance without hardcoding URLs.
		Eventually(func(g Gomega) {
			values, qErr := queryPrometheusInstant(ctx, testCtx.KubeClientset, fmt.Sprintf("kai_queue_allocated_gpus{queue_name=\"%s\"}", queueA.Name))
			g.Expect(qErr).NotTo(HaveOccurred())
			g.Expect(values).NotTo(BeEmpty(), "Expected Prometheus to return at least one sample for queue-a")
			g.Expect(lo.Max(values)).To(BeNumerically(">=", float64(idleGPUs)), "Expected queue-a to be allocating all idle GPUs")
		}, prometheusUsageTimeout, 5*time.Second).Should(Succeed())

		By("Submitting a competing job from queue-b (requires reclaim to schedule)")
		_, queueBPods := pod_group.CreateWithPods(
			ctx,
			testCtx.KubeClientset,
			testCtx.KubeAiSchedClientset,
			utils.GenerateRandomK8sName(10),
			queueB,
			1,
			nil,
			v2alpha2.Preemptible,
			resources,
		)
		podB := queueBPods[0]

		By("Verifying queue-b job gets scheduled due to time-aware fairness reclaim")
		// With time-aware fairness enabled:
		// - queue-a has accumulated historical usage (monopolized ALL GPUs)
		// - queue-b has zero historical usage
		// - queue-b should have higher fairshare and its job should be scheduled
		// - This means one of queue-a's pods must be preempted/reclaimed
		// - If time-aware fairness is NOT working, pod-b will stay pending forever
		Eventually(func(g Gomega) {
			// Refresh pod status
			updatedPodB, err := rd.GetPod(ctx, testCtx.KubeClientset, podB.Namespace, podB.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rd.IsPodScheduled(updatedPodB)).To(BeTrue(),
				"queue-b pod should be scheduled due to time-aware fairness reclaim - "+
					"queue-a's historical usage should result in lower fairshare")
		}, fairnessTimeout, 2*time.Second).Should(Succeed())
	})
})

// createQueuesForTimeAwareFairness creates queues designed to test time-aware fairness:
//
// Setup:
//   - Queue-a: VERY HIGH over-quota weight (10000) → gets ~99.99% base fair share
//   - Queue-b: LOW over-quota weight (1) → gets ~0.01% base fair share
//   - Queue-a monopolizes ALL resources
//
// Without time-aware fairness (kValue=0):
//   - Queue-a fair share ≈ 99.99%, queue-b ≈ 0.01%
//   - Queue-a using 100% is only ~0.01% over fair share
//   - Queue-b's 0.01% fair share is too small to justify reclaiming 1 GPU
//   - Result: NO reclaim, pod-b stays pending
//
// With time-aware fairness (kValue=100, queue-a nUsage=1.0):
//   - Queue-a's weight: max(0, 0.9999 + 100*(0.9999 - 1.0)) = max(0, 0.9999 - 0.01) = 0.9899
//   - Queue-b's weight: max(0, 0.0001 + 100*(0.0001 - 0)) = max(0, 0.0101) = 0.0101
//   - Fair share shifts: queue-a ≈ 99%, queue-b ≈ 1%
//   - With higher kValue, shift is more dramatic
//   - Result: Reclaim happens, pod-b gets scheduled
func createQueuesForTimeAwareFairness() (*v2.Queue, *v2.Queue) {
	queueAName := utils.GenerateRandomK8sName(10)
	queueA := queue.CreateQueueObjectWithGpuResource(queueAName,
		v2.QueueResource{
			Quota:           0,
			OverQuotaWeight: 10000, // VERY HIGH weight - gets ~99.99% base fair share
			Limit:           -1,
		}, "")

	queueBName := utils.GenerateRandomK8sName(10)
	queueB := queue.CreateQueueObjectWithGpuResource(queueBName,
		v2.QueueResource{
			Quota:           0,
			OverQuotaWeight: 1, // LOW weight - gets ~0.01% base fair share
			Limit:           -1,
		}, "")

	return queueA, queueB
}
