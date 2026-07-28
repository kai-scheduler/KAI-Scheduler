// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	waitutils "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	// A disaggregated inference deployment: prefill fills exactly one rack, decode one more, and the
	// front-end is GPU-less. Two racks per deployment tiles the topology tree exactly — a ragged
	// deployment size fragments the free capacity and leaves the last deployment unschedulable under
	// the pod group's required zone constraint.
	inferencePrefillPods  = 2
	inferenceDecodePods   = 4
	inferenceDecodeGPUs   = 4
	inferenceFrontendPods = 2

	inferenceNodesPerDeployment = (inferencePrefillPods*gpusPerNode + inferenceDecodePods*inferenceDecodeGPUs) / gpusPerNode
	inferencePodsPerDeployment  = inferencePrefillPods + inferenceDecodePods + inferenceFrontendPods

	// Elastic victims shrink to half their pods instead of being evicted entirely.
	elasticVictimJobs = 2

	// Each mixed-workload queue runs an RL gang worth ~5% of the cluster.
	mixedWorkloadQueues       = 5
	mixedRLLearnerNodesRatio  = 20 // learner pods = numberOfNodes/20 => 5% of the cluster GPUs
	mixedRLEnvPodsPerLearner  = 2
	mixedPreprocessJobsRatio  = 10 // CPU-only preprocessing jobs per queue = numberOfNodes/10
	mixedSchedulingTargetMins = 2

	// Fractions: half the participating GPUs run 8 pods each, half run 2 => 5 pods per GPU.
	fractionPodsTarget  = 7000
	fractionPodsPerGPU  = 5
	smallGPUFraction    = "0.125"
	smallFractionPerGPU = 8
	largeGPUFraction    = "0.5"
	largeFractionPerGPU = 2
)

// inferenceSubGroups describes one disaggregated inference deployment. zoneLevel/blockLevel/rackLevel are
// node label keys of the cluster topology.
func inferenceSubGroups(topologyName, blockLevel, rackLevel string) []subGroupSpec {
	return []subGroupSpec{
		{
			name:      "prefill",
			pods:      inferencePrefillPods,
			resources: FullNodeGPURequirement,
			topology: &v2alpha2.TopologyConstraint{
				RequiredTopologyLevel: rackLevel,
				Topology:              topologyName,
			},
		},
		{
			name:      "decode",
			pods:      inferenceDecodePods,
			resources: gpuRequirement(inferenceDecodeGPUs),
			topology: &v2alpha2.TopologyConstraint{
				PreferredTopologyLevel: blockLevel,
				Topology:               topologyName,
			},
		},
		{
			name:      "frontend",
			pods:      inferenceFrontendPods,
			resources: CPUOnlyRequirement,
		},
	}
}

// createInferenceDeployments creates deployments concurrently and returns the batch label identifying them.
func createInferenceDeployments(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	deployments int, topologyName, zoneLevel, blockLevel, rackLevel string,
) map[string]string {
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}
	podGroupTopology := &v2alpha2.TopologyConstraint{
		RequiredTopologyLevel: zoneLevel,
		Topology:              topologyName,
	}

	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for range deployments {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()

			_, _, err := createSubGroupPodGroupForKwok(
				ctx, testCtx, testQueue, "inference-"+utils.GenerateRandomK8sName(10),
				podGroupTopology, inferenceSubGroups(topologyName, blockLevel, rackLevel), batchLabels,
			)
			if err != nil {
				lock.Lock()
				defer lock.Unlock()
				creationError = errors.Join(creationError, err)
			}
		}()
	}
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create some inference deployments")

	return batchLabels
}

// disaggregatedInferenceAllocate fills the topology cluster with disaggregated inference deployments and
// measures the time to schedule all of their pods.
func disaggregatedInferenceAllocate(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	totalNodes int, topologyName, zoneLevel, blockLevel, rackLevel string,
) {
	deployments := totalNodes / inferenceNodesPerDeployment
	expectedPods := deployments * inferencePodsPerDeployment

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	batchLabels := createInferenceDeployments(
		ctx, testCtx, testQueue, deployments, topologyName, zoneLevel, blockLevel, rackLevel)

	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)
	endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, expectedPods)

	Expect(writeTestResults("Allocate disaggregated inference deployments", true,
		map[string]interface{}{
			"nodes":       totalNodes,
			"deployments": deployments,
			"pods":        expectedPods,
			"time":        endTime.Sub(startTime).String(),
		})).To(Succeed())
}

// disaggregatedInferenceReclaim submits a single inference deployment into a queue with quota while the
// cluster is full of identically structured deployments in a queue without quota.
func disaggregatedInferenceReclaim(
	ctx context.Context, testCtx *testcontext.TestContext, reclaimQueue *v2.Queue,
	totalNodes int, topologyName, zoneLevel, blockLevel, rackLevel string,
) {
	startTime := time.Now()
	batchLabels := createInferenceDeployments(
		ctx, testCtx, reclaimQueue, 1, topologyName, zoneLevel, blockLevel, rackLevel)
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, inferencePodsPerDeployment)

	Expect(writeTestResults("Reclaim for a disaggregated inference deployment", true,
		map[string]interface{}{
			"nodes":                     totalNodes,
			"pods":                      inferencePodsPerDeployment,
			"time to reclaim (seconds)": endTime.Sub(startTime).Seconds(),
		})).To(Succeed())
}

// elasticJobReclaim fills the cluster with elastic jobs that can shrink to half their pods, then reclaims
// half the cluster for a gang job and verifies the victims were shrunk rather than evicted.
func elasticJobReclaim(
	ctx context.Context, testCtx *testcontext.TestContext,
	victimQueue, reclaimQueue *v2.Queue, numberOfNodes int,
) {
	victimPods := numberOfNodes / elasticVictimJobs
	victimMinMember := victimPods / 2

	var victims []*rd.JobResult
	for range elasticVictimJobs {
		victim, err := createDistributedJobForKwok(ctx, testCtx, victimQueue, rd.DistributedBatchJobOptions{
			Parallelism: ptr.To(int32(victimPods)),
			MinMember:   ptr.To(int32(victimMinMember)),
			Resources:   FullNodeGPURequirement,
			NamePrefix:  "elastic-victim-",
		})
		Expect(err).NotTo(HaveOccurred())
		victims = append(victims, victim)
	}
	waitutils.ForPodCountInNamespace(ctx, testCtx.ControllerClient, victimQueue,
		elasticVictimJobs*victimPods, maxFlowTimeoutMinutes*time.Minute)

	reclaimerPods := numberOfNodes / 2
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	startTime := time.Now()
	_, err := submitDistributedJobForKwok(ctx, testCtx, reclaimQueue, rd.DistributedBatchJobOptions{
		Parallelism: ptr.To(int32(reclaimerPods)),
		Resources:   FullNodeGPURequirement,
		ExtraLabels: batchLabels,
		JobLabels:   batchLabels,
		NamePrefix:  "elastic-reclaimer-",
	})
	Expect(err).NotTo(HaveOccurred())
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, reclaimerPods)

	victimNamespace := queue.GetConnectedNamespaceToQueue(victimQueue)
	for _, victim := range victims {
		waitutils.ForAtLeastNPodsScheduled(
			ctx, testCtx.ControllerClient, victimNamespace, victim.Pods, victimMinMember)
	}

	Expect(writeTestResults("Reclaim from elastic distributed jobs", true,
		map[string]interface{}{
			"nodes":                     numberOfNodes,
			"elastic victim jobs":       elasticVictimJobs,
			"victim pods per job":       victimPods,
			"victim min member":         victimMinMember,
			"reclaimer pods":            reclaimerPods,
			"time to reclaim (seconds)": endTime.Sub(startTime).Seconds(),
		})).To(Succeed())
}

// heroJobReclaim reclaims a full topology domain for one huge job with a required topology constraint.
func heroJobReclaim(
	ctx context.Context, testCtx *testcontext.TestContext, reclaimQueue *v2.Queue,
	heroPods int, topologyName, requiredLevel string,
) {
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	startTime := time.Now()
	_, err := submitDistributedJobForKwok(ctx, testCtx, reclaimQueue, rd.DistributedBatchJobOptions{
		Parallelism: ptr.To(int32(heroPods)),
		Resources:   FullNodeGPURequirement,
		ExtraLabels: batchLabels,
		JobLabels:   batchLabels,
		NamePrefix:  "hero-",
		TopologyConstraint: &v2alpha2.TopologyConstraint{
			RequiredTopologyLevel: requiredLevel,
			Topology:              topologyName,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, heroPods)

	Expect(podsDomains(ctx, testCtx, reclaimQueue, batchLabels, requiredLevel)).
		To(HaveLen(1), "hero job pods must all land in a single %s", requiredLevel)

	Expect(writeTestResults("Hero job reclaim with required topology", true,
		map[string]interface{}{
			"pods":                      heroPods,
			"total requested gpus":      heroPods * gpusPerNode,
			"required topology level":   requiredLevel,
			"time to reclaim (seconds)": endTime.Sub(startTime).Seconds(),
		})).To(Succeed())
}

// mixedWorkloadsScaleTest runs the frontier-lab experiment mix: hierarchical RL gangs (GPU learners plus
// CPU-only environments) next to CPU-only preprocessing batches, spread over several queues.
func mixedWorkloadsScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueues []*v2.Queue, numberOfNodes int,
) {
	learnerPods := max(numberOfNodes/mixedRLLearnerNodesRatio, 1)
	envPods := learnerPods * mixedRLEnvPodsPerLearner
	preprocessJobs := max(numberOfNodes/mixedPreprocessJobsRatio, 1)
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for _, testQueue := range testQueues {
		wg.Add(1)
		go func(testQueue *v2.Queue) {
			defer wg.Done()
			defer GinkgoRecover()

			_, _, err := createSubGroupPodGroupForKwok(
				ctx, testCtx, testQueue, "rl-"+utils.GenerateRandomK8sName(10), nil,
				[]subGroupSpec{
					{name: "learner", pods: learnerPods, resources: FullNodeGPURequirement},
					{name: "env", pods: envPods, resources: CPUOnlyRequirement},
				}, batchLabels)

			for range preprocessJobs {
				_, jobErr := createJobObjectForKwok(ctx, testCtx, testQueue, CPUOnlyRequirement, batchLabels)
				err = errors.Join(err, jobErr)
			}

			if err != nil {
				lock.Lock()
				defer lock.Unlock()
				creationError = errors.Join(creationError, err)
			}
		}(testQueue)
	}
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create some mixed workloads")

	expectedPodsPerQueue := learnerPods + envPods + preprocessJobs
	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)

	lastScheduled := startTime
	for _, testQueue := range testQueues {
		endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, expectedPodsPerQueue)
		if endTime.After(lastScheduled) {
			lastScheduled = endTime
		}
	}

	Expect(writeTestResults("Mixed workloads over multiple queues", true,
		map[string]interface{}{
			"nodes":                      numberOfNodes,
			"queues":                     len(testQueues),
			"rl learner pods per queue":  learnerPods,
			"rl env pods per queue":      envPods,
			"preprocess jobs per queue":  preprocessJobs,
			"max scheduling latency (s)": lastScheduled.Sub(startTime).Seconds(),
			"target latency (s)":         mixedSchedulingTargetMins * 60,
		})).To(Succeed())
}

// fractionsScaleTest allocates thousands of short fractional-GPU pods, mixing two fraction sizes.
func fractionsScaleTest(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, numberOfNodes int,
) {
	fractionGPUs := min(fractionPodsTarget/fractionPodsPerGPU, numberOfNodes*gpusPerNode)
	smallPods := (fractionGPUs / 2) * smallFractionPerGPU
	largePods := (fractionGPUs / 2) * largeFractionPerGPU
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	createFractionPods := func(count int, fraction string) {
		for range count {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer GinkgoRecover()

				if _, err := createFractionJobForKwok(ctx, testCtx, testQueue, fraction, batchLabels); err != nil {
					lock.Lock()
					defer lock.Unlock()
					creationError = errors.Join(creationError, err)
				}
			}()
		}
	}
	createFractionPods(smallPods, smallGPUFraction)
	createFractionPods(largePods, largeGPUFraction)
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create some fraction jobs")

	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)
	endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, smallPods+largePods)

	Expect(writeTestResults("Allocate fractional GPU jobs", true,
		map[string]interface{}{
			"nodes":                    numberOfNodes,
			"gpus":                     fractionGPUs,
			smallGPUFraction + " pods": smallPods,
			largeGPUFraction + " pods": largePods,
			"time":                     endTime.Sub(startTime).String(),
		})).To(Succeed())
}

// waitForBatchToSchedule waits until expectedPods labeled with batchLabels are scheduled in the queue
// namespace and returns the time the last of them was scheduled. Unlike waitForAllJobsToSchedule it
// ignores other workloads sharing the queue.
func waitForBatchToSchedule(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	batchLabels map[string]string, expectedPods int,
) time.Time {
	namespace := queue.GetConnectedNamespaceToQueue(testQueue)
	podsList := &v1.PodList{}

	Eventually(func(g Gomega) {
		g.Expect(testCtx.ControllerClient.List(ctx, podsList,
			runtimeClient.InNamespace(namespace),
			runtimeClient.MatchingLabels(batchLabels))).To(Succeed())
		g.Expect(podsList.Items).To(HaveLen(expectedPods))

		for _, pod := range podsList.Items {
			g.Expect(rd.IsPodScheduled(&pod)).To(BeTrue(), "pod %s is not scheduled", pod.Name)
		}
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	var lastScheduledTime time.Time
	for _, pod := range podsList.Items {
		scheduledTime, err := getPodScheduledTime(&pod)
		if err != nil {
			Fail(fmt.Sprintf("Expected all pods to be scheduled but pod %s is not scheduled", pod.Name))
		}
		if lastScheduledTime.Before(scheduledTime) {
			lastScheduledTime = scheduledTime
		}
	}
	return lastScheduledTime
}

// podsDomains returns the distinct values of the given topology node label across the batch's pods.
func podsDomains(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	batchLabels map[string]string, nodeLabel string,
) []string {
	podsList := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, podsList,
		runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
		runtimeClient.MatchingLabels(batchLabels))).To(Succeed())

	nodes := &v1.NodeList{}
	Expect(testCtx.ControllerClient.List(ctx, nodes)).To(Succeed())
	domainByNode := map[string]string{}
	for _, node := range nodes.Items {
		domainByNode[node.Name] = node.Labels[nodeLabel]
	}

	var domains []string
	seen := map[string]bool{}
	for _, pod := range podsList.Items {
		domain := domainByNode[pod.Spec.NodeName]
		if !seen[domain] {
			seen[domain] = true
			domains = append(domains, domain)
		}
	}
	return domains
}
