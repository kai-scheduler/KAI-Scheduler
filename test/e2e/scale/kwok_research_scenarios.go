// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	. "github.com/onsi/gomega"
	kwok "github.com/run-ai/kwok-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
)

const (
	researchCPUNodePoolName  = "research-cpu"
	researchCPUPoolLabel     = "scale-test-node-pool"
	researchCPUNodeCPUs      = 64
	researchWorkloadsPerKind = 12
	rlGeneratorGPUs          = 4

	researchDecisionP95ScenarioName = "Research workloads - decision latency p95"
	researchDecisionMaxScenarioName = "Research workloads - decision latency max"
)

type researchWorkloadKind string

const (
	experimentWorkload researchWorkloadKind = "experiment"
	rlWorkload         researchWorkloadKind = "rl"
	preprocessWorkload researchWorkloadKind = "preprocess"
)

type researchWorkload struct {
	kind      researchWorkloadKind
	subGroups []subGroupSpec
}

// researchDecision is the scheduler's first decision on a workload: allocated or unschedulable.
type researchDecision struct {
	kind      researchWorkloadKind
	scheduled bool
	gpus      int64
	latency   float64
}

var preprocessRequirement = v1.ResourceRequirements{
	Limits: v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("8"),
		v1.ResourceMemory: resource.MustParse("32Gi"),
	},
}

func researchCPUNodeCount(gpuNodes int) int {
	return max(1, gpuNodes/5)
}

func researchQueueCount(gpuNodes int) int {
	return max(4, gpuNodes/125)
}

func percentOf(total, pct int) int {
	return max(1, total*pct/100)
}

// researchWorkloadMix sizes every workload at 5-10% of the GPU or CPU capacity, so pod counts grow with the cluster.
// Experiments and RL together request ~150% of the GPUs, RL environments and preprocessing ~150% of the CPU pool.
func researchWorkloadMix(gpuNodes, cpuNodes int) []researchWorkload {
	cpus := cpuNodes * researchCPUNodeCPUs
	cpuSlots := func(requirement v1.ResourceRequirements) int {
		return cpus / int(requirement.Limits.Cpu().Value())
	}
	cpuPool := map[string]string{researchCPUPoolLabel: researchCPUNodePoolName}

	workloads := make([]researchWorkload, 0, 3*researchWorkloadsPerKind)
	for i := range researchWorkloadsPerKind {
		pct := 5 + 5*(i%2)
		workloads = append(workloads,
			researchWorkload{kind: experimentWorkload, subGroups: []subGroupSpec{
				{name: "workers", pods: percentOf(gpuNodes, pct), resources: FullNodeGPURequirement},
			}},
			researchWorkload{kind: rlWorkload, subGroups: []subGroupSpec{
				{name: "trainer", pods: percentOf(gpuNodes, 3), resources: FullNodeGPURequirement},
				{name: "rollout"},
				{name: "generator", parent: "rollout", pods: percentOf(gpuNodes*gpusPerNode/rlGeneratorGPUs, 2),
					resources: gpuRequirement(rlGeneratorGPUs)},
				{name: "envs", parent: "rollout", pods: percentOf(cpuSlots(cpuOnlyRequirement), 5),
					resources: cpuOnlyRequirement, nodeSelector: cpuPool},
			}},
			researchWorkload{kind: preprocessWorkload, subGroups: []subGroupSpec{
				{name: "workers", pods: percentOf(cpuSlots(preprocessRequirement), pct),
					resources: preprocessRequirement, nodeSelector: cpuPool},
			}},
		)
	}
	return workloads
}

func (w researchWorkload) pods() int {
	total := 0
	for _, subGroup := range w.subGroups {
		total += subGroup.pods
	}
	return total
}

func (w researchWorkload) demand() (gpus, cpus int64) {
	for _, subGroup := range w.subGroups {
		limits := subGroup.resources.Limits
		gpus += int64(subGroup.pods) * limits.Name(constants.NvidiaGpuResource, resource.DecimalSI).Value()
		cpus += int64(subGroup.pods) * limits.Cpu().Value()
	}
	return gpus, cpus
}

// createResearchCPUNodePool adds CPU-only KWOK nodes. Without the simulated GPU node pool label, fake-gpu-operator
// gives them no GPUs.
func createResearchCPUNodePool(ctx context.Context, testCtx *testcontext.TestContext, nodes int) {
	labels := map[string]string{"type": "kwok", researchCPUPoolLabel: researchCPUNodePoolName}
	resources := v1.ResourceList{
		v1.ResourceCPU:    *resource.NewQuantity(researchCPUNodeCPUs, resource.DecimalSI),
		v1.ResourceMemory: resource.MustParse("512Gi"),
		v1.ResourcePods:   resource.MustParse("110"),
	}
	nodePool := &kwok.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: researchCPUNodePoolName, Labels: labels},
		Spec: kwok.NodePoolSpec{
			NodeCount: int32(nodes),
			NodeTemplate: v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Status: v1.NodeStatus{
					Allocatable: resources,
					Capacity:    resources,
					NodeInfo: v1.NodeSystemInfo{
						Architecture:     "amd64",
						KubeProxyVersion: "fake",
						KubeletVersion:   "fake",
						OperatingSystem:  "linux",
					},
					Phase: "Running",
				},
			},
		},
	}
	Expect(testCtx.ControllerClient.Create(ctx, nodePool)).To(Succeed())
	wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, researchCPUNodePoolName)
	Eventually(func(g Gomega) {
		g.Expect(researchCPUNodeNames(ctx, g, testCtx)).To(HaveLen(nodes))
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())
}

func deleteResearchCPUNodePool(ctx context.Context, testCtx *testcontext.TestContext) {
	nodePool := &kwok.NodePool{ObjectMeta: metav1.ObjectMeta{Name: researchCPUNodePoolName}}
	Expect(runtimeClient.IgnoreNotFound(testCtx.ControllerClient.Delete(ctx, nodePool))).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(researchCPUNodeNames(ctx, g, testCtx)).To(BeEmpty())
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())
}

func researchCPUNodeNames(ctx context.Context, g Gomega, testCtx *testcontext.TestContext) map[string]bool {
	nodes := &v1.NodeList{}
	g.Expect(testCtx.ControllerClient.List(ctx, nodes,
		runtimeClient.MatchingLabels{researchCPUPoolLabel: researchCPUNodePoolName})).To(Succeed())
	names := make(map[string]bool, len(nodes.Items))
	for _, node := range nodes.Items {
		names[node.Name] = true
	}
	return names
}

// researchWorkloads submits a mix of experiments, hierarchical RL pipelines and CPU-only preprocessing jobs across
// the queues while the scheduler is paused, then measures how long each workload waits for its first scheduling
// decision: every pod allocated, or the PodGroup marked unschedulable.
func researchWorkloads(
	ctx context.Context, testCtx *testcontext.TestContext, queues []*v2.Queue, gpuNodes, cpuNodes int,
) {
	workloads := researchWorkloadMix(gpuNodes, cpuNodes)
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	byPodGroup := make(map[string]researchWorkload, len(workloads))
	expectedPods := 0
	for i, workload := range workloads {
		name := fmt.Sprintf("%s-%d-%s", workload.kind, i, utils.GenerateRandomK8sName(6))
		_, _, err := createSubGroupPodGroupForKwok(ctx, testCtx, queues[i%len(queues)], name, nil,
			workload.subGroups, batchLabels)
		Expect(err).NotTo(HaveOccurred(), "failed to create research workload %s", name)
		byPodGroup[name] = workload
		expectedPods += workload.pods()
	}

	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)

	var decisions []researchDecision
	var boundPods []v1.Pod
	Eventually(func(g Gomega) {
		decisions, boundPods = nil, nil
		totalPods := 0
		for _, testQueue := range queues {
			queueDecisions, queuePods, podCount := researchQueueDecisions(
				ctx, g, testCtx, testQueue, batchLabels, byPodGroup, startTime)
			decisions = append(decisions, queueDecisions...)
			boundPods = append(boundPods, queuePods...)
			totalPods += podCount
		}
		g.Expect(totalPods).To(Equal(expectedPods), "not all pods were created")
		g.Expect(decisions).To(HaveLen(len(workloads)),
			"%d of %d workloads have no scheduling decision yet", len(workloads)-len(decisions), len(workloads))
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	validateResearchPlacement(ctx, testCtx, boundPods)

	var allocatedGPUs int64
	latencies := map[researchWorkloadKind][]float64{}
	var all []float64
	scheduled := map[researchWorkloadKind]int{}
	for _, decision := range decisions {
		latencies[decision.kind] = append(latencies[decision.kind], decision.latency)
		all = append(all, decision.latency)
		if decision.scheduled {
			scheduled[decision.kind]++
			allocatedGPUs += decision.gpus
		}
	}
	for _, kind := range []researchWorkloadKind{experimentWorkload, rlWorkload, preprocessWorkload} {
		Expect(scheduled[kind]).To(BeNumerically(">", 0), "no %s workload was allocated", kind)
		sort.Float64s(latencies[kind])
	}
	sort.Float64s(all)
	scheduledTotal := scheduled[experimentWorkload] + scheduled[rlWorkload] + scheduled[preprocessWorkload]

	details := map[string]interface{}{
		"nodes":                           gpuNodes,
		"cpu_nodes":                       cpuNodes,
		"queues":                          len(queues),
		"workloads":                       len(workloads),
		"pods":                            expectedPods,
		"scheduled_workloads":             scheduledTotal,
		"unschedulable_workloads":         len(workloads) - scheduledTotal,
		"gpu_utilization_pct":             float64(allocatedGPUs) / float64(gpuNodes*gpusPerNode) * 100,
		"decision_p50_seconds":            percentile(all, 50),
		"decision_p95_seconds":            percentile(all, 95),
		"decision_max_seconds":            percentile(all, 100),
		"experiment_decision_p95_seconds": percentile(latencies[experimentWorkload], 95),
		"rl_decision_p95_seconds":         percentile(latencies[rlWorkload], 95),
		"preprocess_decision_p95_seconds": percentile(latencies[preprocessWorkload], 95),
	}
	Expect(writeTestResults(researchDecisionP95ScenarioName, true, details)).To(Succeed())
	Expect(writeTestResults(researchDecisionMaxScenarioName, true, details)).To(Succeed())
}

// researchQueueDecisions returns the decided workloads in the queue, their bound pods and the queue's pod count.
// A workload counts as allocated once all of its pods have BindRequests, and must be all-or-nothing.
func researchQueueDecisions(
	ctx context.Context, g Gomega, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	batchLabels map[string]string, byPodGroup map[string]researchWorkload, startTime time.Time,
) ([]researchDecision, []v1.Pod, int) {
	namespace := queue.GetConnectedNamespaceToQueue(testQueue)
	podGroups := &v2alpha2.PodGroupList{}
	g.Expect(testCtx.ControllerClient.List(ctx, podGroups, runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingLabels(batchLabels))).To(Succeed())
	pods := &v1.PodList{}
	g.Expect(testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingLabels(batchLabels))).To(Succeed())
	allocatedAt := bindRequestCreationTimes(ctx, g, testCtx, namespace)

	type allocation struct {
		pods int
		last time.Time
	}
	allocations := map[string]*allocation{}
	var boundPods []v1.Pod
	for _, pod := range pods.Items {
		createdAt, hasBindRequest := allocatedAt[pod.Name]
		if !hasBindRequest {
			continue
		}
		g.Expect(rd.IsPodScheduled(&pod)).To(BeTrue(), "allocated pod %s is not bound yet", pod.Name)
		boundPods = append(boundPods, pod)
		podGroupName := pod.Labels[rd.PodGroupLabelName]
		if allocations[podGroupName] == nil {
			allocations[podGroupName] = &allocation{}
		}
		allocations[podGroupName].pods++
		if createdAt.After(allocations[podGroupName].last) {
			allocations[podGroupName].last = createdAt
		}
	}

	var decisions []researchDecision
	for _, podGroup := range podGroups.Items {
		workload := byPodGroup[podGroup.Name]
		var decidedAt time.Time
		scheduled := false
		if allocated := allocations[podGroup.Name]; allocated != nil {
			g.Expect(allocated.pods).To(Equal(workload.pods()), "workload %s is partially allocated", podGroup.Name)
			decidedAt, scheduled = allocated.last, true
		}
		for _, condition := range podGroup.Status.SchedulingConditions {
			if condition.Type == v2alpha2.UnschedulableOnNodePool &&
				(decidedAt.IsZero() || condition.LastTransitionTime.Time.Before(decidedAt)) {
				decidedAt = condition.LastTransitionTime.Time
			}
		}
		if decidedAt.IsZero() {
			continue
		}
		gpus, _ := workload.demand()
		decisions = append(decisions, researchDecision{
			kind:      workload.kind,
			scheduled: scheduled,
			gpus:      gpus,
			latency:   math.Max(decidedAt.Sub(startTime).Seconds(), 0),
		})
	}
	return decisions, boundPods, len(pods.Items)
}

// validateResearchPlacement checks that CPU-only pods ran on the CPU pool and GPU pods did not.
func validateResearchPlacement(ctx context.Context, testCtx *testcontext.TestContext, pods []v1.Pod) {
	cpuNodes := researchCPUNodeNames(ctx, Default, testCtx)
	for _, pod := range pods {
		requestsGPU := false
		for _, container := range pod.Spec.Containers {
			if _, found := container.Resources.Limits[constants.NvidiaGpuResource]; found {
				requestsGPU = true
			}
		}
		Expect(cpuNodes[pod.Spec.NodeName]).To(Equal(!requestsGPU),
			"pod %s (GPU: %t) is on node %s", pod.Name, requestsGPU, pod.Spec.NodeName)
	}
}
