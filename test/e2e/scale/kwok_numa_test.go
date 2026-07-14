// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kwok "github.com/run-ai/kwok-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	// numaZonesPerNode matches the fake-gpu-operator NUMA profile (2 zones × 4 GPU on an 8-GPU node).
	numaZonesPerNode = 2
	// numaVictimGPUsPerPod fills a 4-GPU zone to 3, leaving a 1-GPU gap. With one victim per zone every
	// node has 2 free GPU, but split 1+1 across zones — unusable by a single-zone request.
	numaVictimGPUsPerPod = 3
	// numaPreemptorGPUsPerPod needs 2 GPU in one NUMA zone. The node's 2 free GPU (1 per zone) pass the
	// coarse node-level resource check but fail NUMA alignment, so the preemptor is only feasible after a
	// 3-GPU victim is evicted to open a full zone — exercising NUMA-forced preemption, not a plain full node.
	numaPreemptorGPUsPerPod = 2

	// numaRoleLabelKey distinguishes preemptors from victims when they share a queue, so the preempt
	// scenario can wait on the preemptors alone (evicted victims stay pending on the full cluster).
	numaRoleLabelKey  = "numa-role"
	numaRoleVictim    = "victim"
	numaRolePreemptor = "preemptor"
)

// Kwok NUMA scale test exercises the numa plugin's per-(task,node) predicate fan-out (allocate) and its
// eviction / zone-crediting path (preempt) at cluster scale. Requires the fake-gpu-operator to publish
// NodeResourceTopology (installed with the NRT profile by hack/setup-scale-test-env.sh); skips otherwise.
// The numa plugin only engages Guaranteed pods, so all workloads set requests==limits on cpu/memory/gpu.
var _ = Describe("Kwok NUMA scale test", Ordered, Label(labels.Scale), func() {
	var (
		testCtx *testcontext.TestContext

		parentQueue       *v2.Queue
		numaAllocateQueue *v2.Queue
		numaPreemptQueue  *v2.Queue
		numaGangQueue     *v2.Queue

		preemptiblePriorityClass    string
		nonPreemptiblePriorityClass string
		numberOfNodes               int
	)

	BeforeAll(func(ctx context.Context) {
		numberOfNodes = defaultNumberOfNodes
		if nodeCountEnvValue := os.Getenv("NODE_COUNT"); len(nodeCountEnvValue) > 0 {
			if value, err := strconv.Atoi(nodeCountEnvValue); err == nil {
				numberOfNodes = value
			} else {
				GinkgoLogr.Error(err, "failed to read NODE_COUNT environment variable")
			}
		}

		testCtx = testcontext.GetConnectivity(ctx, Default)

		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, "nodepools.kwok.sigs.run-ai.com", "v1beta1")
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, "noderesourcetopologies.topology.node.k8s.io", "v1alpha2")

		queues := v2.QueueList{}
		Expect(testCtx.ControllerClient.List(ctx, &queues)).To(Succeed())
		for i := range queues.Items {
			cleanupTestQueue(ctx, testCtx, &queues.Items[i])
		}

		schedulerconfig.EnableScheduler(ctx, testCtx)
		Expect(feature_flags.EnableNUMA(ctx, testCtx, map[string]string{"reconstructAvailable": "true"})).To(Succeed())

		var err error
		preemptiblePriorityClass, nonPreemptiblePriorityClass, err = rd.CreatePreemptibleAndNonPriorityClass(ctx, testCtx.KubeClientset)
		Expect(err).NotTo(HaveOccurred())

		parentQueue = queue.CreateQueueObject("numa-parent-"+utils.GenerateRandomK8sName(10), "")
		numaAllocateQueue = queue.CreateQueueObject("numa-allocate-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
		numaPreemptQueue = queue.CreateQueueObject("numa-preempt-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
		numaGangQueue = queue.CreateQueueObject("numa-gang-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{parentQueue, numaAllocateQueue, numaPreemptQueue, numaGangQueue})

		setupNumaNodePool(ctx, testCtx, numberOfNodes)
	})

	AfterAll(func(ctx context.Context) {
		if CurrentSpecReport().Failed() {
			return
		}
		for _, queueToClean := range testCtx.Queues {
			cleanupTestQueue(ctx, testCtx, queueToClean)
		}
		Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
		wait.ForNoE2EPods(ctx, testCtx.ControllerClient)
		testCtx.ClusterCleanup(ctx)
		Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
	})

	AfterEach(func(ctx context.Context) {
		if CurrentSpecReport().Failed() {
			return
		}
		for _, queueToClean := range testCtx.Queues {
			cleanupTestQueue(ctx, testCtx, queueToClean)
		}
	})

	It("NUMA allocate: fill cluster with Guaranteed single-GPU jobs", func(ctx context.Context) {
		numaAllocateScaleTest(ctx, testCtx, numaAllocateQueue, numberOfNodes)
	}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

	It("NUMA preempt: high-priority pods free fragmented zones", func(ctx context.Context) {
		numaPreemptScaleTest(ctx, testCtx, numaPreemptQueue, preemptiblePriorityClass, nonPreemptiblePriorityClass, numberOfNodes)
	}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

	It("NUMA gang preempt: one huge distributed job preempts across zones", func(ctx context.Context) {
		numaGangPreemptScaleTest(ctx, testCtx, numaGangQueue, preemptiblePriorityClass, nonPreemptiblePriorityClass, numberOfNodes)
	}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))
})

// setupNumaNodePool cycles the managed kwok node pool to numberOfNodes, mirroring the "Big cluster"
// context: scale to 0 first so every node is (re)created fresh with the fake-gpu-operator's NRT.
func setupNumaNodePool(ctx context.Context, testCtx *testcontext.TestContext, numberOfNodes int) {
	updateFakeGPUOperatorGPUsPerNode(ctx, testCtx)

	managedNodePool := kwok.NodePool{ObjectMeta: metav1.ObjectMeta{Name: KWOKOperatorNodePoolName}}
	Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(&managedNodePool), &managedNodePool)).To(Succeed())

	original := managedNodePool.DeepCopy()
	managedNodePool.Spec.NodeCount = int32(0)
	Expect(testCtx.ControllerClient.Patch(ctx, &managedNodePool, runtimeClient.MergeFrom(original))).NotTo(HaveOccurred())
	wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, managedNodePool.Name)
	wait.ForZeroKWOKNodes(ctx, testCtx.ControllerClient)

	original = managedNodePool.DeepCopy()
	managedNodePool.Spec.NodeCount = int32(numberOfNodes)
	GinkgoLogr.Info("Setting up NUMA node pool", "numberOfNodes", numberOfNodes, "gpusPerNode", gpusPerNode)
	Expect(testCtx.ControllerClient.Patch(ctx, &managedNodePool, runtimeClient.MergeFrom(original))).NotTo(HaveOccurred())
	wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, managedNodePool.Name)
	wait.ForGPUOPeratorUpdateOnKWOKNodes(ctx, testCtx.ControllerClient, numberOfNodes, gpusPerNode)
}

// numaAllocateScaleTest fills the cluster to capacity with Guaranteed single-GPU jobs. Every task is
// NUMA-handled and feasible on every node, maximizing the per-(task,node) predicate fan-out — the
// evaluate/clone hot path. Measures wall-clock time to schedule the whole backlog.
func numaAllocateScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, numberOfNodes int,
) {
	startTime, endTime, totalNumberOfJobs := fillClusterWithJobs(
		ctx, testCtx, testQueue, true, numberOfNodes, numautil.GuaranteedGPURequirements(1),
	)

	GinkgoLogr.Info(
		"NUMA allocate scheduled", "Total time", endTime.Sub(startTime),
		"nodes", numberOfNodes, "jobs", totalNumberOfJobs,
	)

	Expect(writeTestResults("NUMA allocate - fill cluster with Guaranteed single-GPU jobs", true,
		map[string]interface{}{
			"nodes": numberOfNodes,
			"jobs":  totalNumberOfJobs,
			"time":  endTime.Sub(startTime).String(),
		})).To(Succeed())
}

// numaPreemptScaleTest puts one preemptible 3-GPU Guaranteed victim in each NUMA zone, leaving a 1-GPU
// gap per zone (2 free GPU per node, fragmented 1+1). It then submits higher-priority 2-GPU Guaranteed
// preemptors: the request passes the node's coarse GPU check but no single zone has 2 free, so each is
// only feasible after evicting one 3-GPU victim to open a full zone. This forces NUMA-driven preemption —
// a plain scheduler would bind into the free GPU — exercising the plugin's zone-crediting and
// eviction-dedup at scale. Measures wall-clock time for the preemptors to schedule.
func numaPreemptScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	preemptiblePriorityClass, nonPreemptiblePriorityClass string, numberOfNodes int,
) {
	victimCount := fillZonesWithVictims(ctx, testCtx, testQueue, preemptiblePriorityClass, numberOfNodes)

	// Preemptors must outrank the victims: the preemptible class draws a priority below the
	// non-preemptible threshold, the non-preemptible class above it, so preemptors both outrank the
	// victims and are themselves not preemptible.
	preemptorCount := numberOfNodes
	startTime := time.Now()
	createGuaranteedGPUJobsForKwok(
		ctx, testCtx, testQueue, preemptorCount, numaPreemptorGPUsPerPod,
		nonPreemptiblePriorityClass, map[string]string{numaRoleLabelKey: numaRolePreemptor},
	)
	endTime := waitForScheduledPodsWithLabel(ctx, testCtx, testQueue, numaRoleLabelKey, numaRolePreemptor, preemptorCount)

	GinkgoLogr.Info(
		"NUMA preempt scheduled", "Total time", endTime.Sub(startTime),
		"nodes", numberOfNodes, "victims", victimCount, "preemptors", preemptorCount,
	)

	Expect(writeTestResults("NUMA preempt - free fragmented zones for higher-priority pods", true,
		map[string]interface{}{
			"nodes":                  numberOfNodes,
			"victims":                victimCount,
			"preemptors":             preemptorCount,
			"time to preempt (secs)": endTime.Sub(startTime).Seconds(),
		})).To(Succeed())
}

// numaGangPreemptScaleTest is the gang variant of numaPreemptScaleTest: instead of many independent
// preemptor pods, one distributed job (gang — MinMember == parallelism) of many 2-GPU pods must be
// placed atomically. Each pod still needs a full zone opened, so the scheduler has to find a single
// coordinated set of victim evictions across many nodes — stressing the multi-node gang scenario
// search on top of the NUMA predicate, the path that dominated profiling. Measures wall-clock time
// for the whole gang to schedule.
func numaGangPreemptScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	preemptiblePriorityClass, nonPreemptiblePriorityClass string, numberOfNodes int,
) {
	victimCount := fillZonesWithVictims(ctx, testCtx, testQueue, preemptiblePriorityClass, numberOfNodes)

	gangSize := numaGangSize(numberOfNodes)
	startTime := time.Now()
	createGuaranteedGPUGangForKwok(
		ctx, testCtx, testQueue, gangSize, numaPreemptorGPUsPerPod,
		nonPreemptiblePriorityClass, map[string]string{numaRoleLabelKey: numaRolePreemptor},
	)
	endTime := waitForScheduledPodsWithLabel(ctx, testCtx, testQueue, numaRoleLabelKey, numaRolePreemptor, gangSize)

	GinkgoLogr.Info(
		"NUMA gang preempt scheduled", "Total time", endTime.Sub(startTime),
		"nodes", numberOfNodes, "victims", victimCount, "gang pods", gangSize,
	)

	Expect(writeTestResults("NUMA gang preempt - one distributed job preempts across zones", true,
		map[string]interface{}{
			"nodes":                  numberOfNodes,
			"victims":                victimCount,
			"gang pods":              gangSize,
			"time to preempt (secs)": endTime.Sub(startTime).Seconds(),
		})).To(Succeed())
}

// fillZonesWithVictims puts one preemptible 3-GPU Guaranteed victim in each NUMA zone of every node
// (scheduler disabled during creation for speed), leaving a 1-GPU gap per zone. Returns the victim count.
func fillZonesWithVictims(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	preemptiblePriorityClass string, numberOfNodes int,
) int {
	// One 3-GPU victim per zone: both zones of every node carry a victim with a 1-GPU gap.
	victimCount := numberOfNodes * numaZonesPerNode

	schedulerconfig.DisableScheduler(ctx, testCtx)
	createGuaranteedGPUJobsForKwok(
		ctx, testCtx, testQueue, victimCount, numaVictimGPUsPerPod,
		preemptiblePriorityClass, map[string]string{numaRoleLabelKey: numaRoleVictim},
	)
	wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, metav1.LabelSelector{
		MatchLabels: map[string]string{testconfig.GetConfig().QueueLabelKey: testQueue.Name},
	}, victimCount)
	schedulerconfig.EnableScheduler(ctx, testCtx)
	waitForAllJobsToSchedule(ctx, testCtx, testQueue, victimCount)

	return victimCount
}

// numaGangSize is the number of pods in the gang preemptor job — the stress knob. Defaults to one pod
// per node (a cluster-spanning gang) and is overridable via NUMA_GANG_SIZE. A larger gang forces a
// bigger atomic set of coordinated evictions; reduce it if the gang cannot be solved within the
// scheduler's multi-node gang search budget.
func numaGangSize(numberOfNodes int) int {
	size := numberOfNodes
	if v := os.Getenv("NUMA_GANG_SIZE"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			size = parsed
		} else {
			GinkgoLogr.Error(err, "failed to read NUMA_GANG_SIZE environment variable")
		}
	}
	return size
}

// createGuaranteedGPUGangForKwok creates one distributed (gang — MinMember defaults to parallelism)
// Guaranteed GPU job on the kwok nodes: all pods schedule together or none do.
func createGuaranteedGPUGangForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	pods, gpusPerPod int, priorityClassName string, extraLabels map[string]string,
) {
	_, _, _, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testQueue,
		rd.DistributedBatchJobOptions{
			Parallelism:       ptr.To(int32(pods)),
			Resources:         numautil.GuaranteedGPURequirements(int64(gpusPerPod)),
			PriorityClassName: priorityClassName,
			ExtraLabels:       extraLabels,
			PodSpecMutator:    addKWOKTaintsAndAffinity,
		})
	Expect(err).To(Succeed())
}

// createGuaranteedGPUJobsForKwok concurrently creates count single-pod Guaranteed GPU jobs on the kwok
// nodes, each requesting gpusPerPod GPUs with the given priority class and extra pod labels.
func createGuaranteedGPUJobsForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	count, gpusPerPod int, priorityClassName string, extraLabels map[string]string,
) {
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			_, _, _, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testQueue,
				rd.DistributedBatchJobOptions{
					Resources:         numautil.GuaranteedGPURequirements(int64(gpusPerPod)),
					PriorityClassName: priorityClassName,
					ExtraLabels:       extraLabels,
					PodSpecMutator:    addKWOKTaintsAndAffinity,
				})
			Expect(err).To(Succeed())
		}()
	}
	wg.Wait()
}

// waitForScheduledPodsWithLabel waits until exactly expected pods carrying labelKey=labelVal in the
// queue's namespace are scheduled, returning the latest schedule time. Unlike waitForAllJobsToSchedule
// it ignores other pods in the queue (e.g. victims left pending after preemption).
func waitForScheduledPodsWithLabel(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	labelKey, labelVal string, expected int,
) time.Time {
	namespace := queue.GetConnectedNamespaceToQueue(testQueue)
	var lastScheduledTime time.Time

	Eventually(func(g Gomega) {
		pods := &v1.PodList{}
		g.Expect(testCtx.ControllerClient.List(ctx, pods,
			runtimeClient.InNamespace(namespace),
			runtimeClient.MatchingLabels{labelKey: labelVal},
		)).To(Succeed())

		scheduled := 0
		for i := range pods.Items {
			scheduledTime, err := getPodScheduledTime(&pods.Items[i])
			if err != nil {
				continue
			}
			scheduled++
			if scheduledTime.After(lastScheduledTime) {
				lastScheduledTime = scheduledTime
			}
		}
		g.Expect(scheduled).To(Equal(expected))
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	return lastScheduledTime
}
