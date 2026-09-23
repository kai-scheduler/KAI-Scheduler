// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	waitutils "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/scale/topology"
)

const (
	topologyZoneLabel  = "cloud.provider.com/topology-zone"
	topologyBlockLabel = "cloud.provider.com/topology-block"
	topologyRackLabel  = "cloud.provider.com/topology-rack"

	minimumTopologyNodeCount          = 512
	topologyZones                     = 2
	topologyBlocksPerZone             = 8
	topologyNodesPerRack              = 2
	legacyTopologyWorkloadPods        = 512
	topologyNodePoolCreateConcurrency = 64

	inferenceDeploymentLabel = "scale-test-inference-deployment"
	inferencePrefillPods     = 2
	inferenceDecodePods      = 4
	inferenceDecodeGPUs      = 4
	inferenceFrontendPods    = 2

	elasticReclaimVerificationTimeout = 5 * time.Minute
)

var cpuOnlyRequirement = v1.ResourceRequirements{
	Limits: v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("2"),
		v1.ResourceMemory: resource.MustParse("4Gi"),
	},
}

type topologyScaleConfig struct {
	zones         int
	blocksPerZone int
	racksPerBlock int
	nodesPerRack  int
}

func topologyNodeCount(requestedNodes int) (int, error) {
	if requestedNodes <= 0 {
		return 0, fmt.Errorf("node count must be positive, got %d", requestedNodes)
	}

	nodes := minimumTopologyNodeCount
	maxInt := int(^uint(0) >> 1)
	for nodes < requestedNodes {
		if nodes > maxInt/2 {
			return 0, fmt.Errorf("node count %d is too large to round up to a power of two", requestedNodes)
		}
		nodes *= 2
	}
	return nodes, nil
}

func topologyConfigForNodeCount(nodes int) (topologyScaleConfig, error) {
	nodeCountDivisor := topologyZones * topologyBlocksPerZone * topologyNodesPerRack
	if nodes < minimumTopologyNodeCount || nodes%nodeCountDivisor != 0 {
		return topologyScaleConfig{}, fmt.Errorf(
			"topology node count %d must be at least %d and divisible by %d",
			nodes, minimumTopologyNodeCount, nodeCountDivisor,
		)
	}

	return topologyScaleConfig{
		zones:         topologyZones,
		blocksPerZone: topologyBlocksPerZone,
		racksPerBlock: nodes / nodeCountDivisor,
		nodesPerRack:  topologyNodesPerRack,
	}, nil
}

func (config topologyScaleConfig) levels() []topology.TopologyLevel {
	return []topology.TopologyLevel{
		{Name: topologyZoneLabel, Count: config.zones, ShortName: "zone"},
		{Name: topologyBlockLabel, Count: config.blocksPerZone, ShortName: "block"},
		{Name: topologyRackLabel, Count: config.racksPerBlock, ShortName: "rack"},
	}
}

func (config topologyScaleConfig) totalNodes() int {
	return config.zones * config.blocksPerZone * config.racksPerBlock * config.nodesPerRack
}

func (config topologyScaleConfig) nodesPerZone() int {
	return config.totalNodes() / config.zones
}

func (config topologyScaleConfig) nodePoolCount() int {
	return config.zones * config.blocksPerZone * config.racksPerBlock
}

func splitClusterForElasticReclaim(nodes int) (victimMinMember, reclaimerPods int, err error) {
	if nodes <= 0 {
		return 0, 0, fmt.Errorf("node count must be positive, got %d", nodes)
	}
	victimMinMember = nodes / 2
	reclaimerPods = nodes - victimMinMember
	return victimMinMember, reclaimerPods, nil
}

func inferenceNodesPerDeployment() int {
	return (inferencePrefillPods*gpusPerNode + inferenceDecodePods*inferenceDecodeGPUs) / gpusPerNode
}

func inferencePodsPerDeployment() int {
	return inferencePrefillPods + inferenceDecodePods + inferenceFrontendPods
}

func inferenceDeploymentCount(nodes int) (int, error) {
	nodesPerDeployment := inferenceNodesPerDeployment()
	if nodes <= 0 || nodes%nodesPerDeployment != 0 {
		return 0, fmt.Errorf("node count %d must be positive and divisible by %d", nodes, nodesPerDeployment)
	}
	return nodes / nodesPerDeployment, nil
}

func gpuRequirement(gpus int) v1.ResourceRequirements {
	return v1.ResourceRequirements{
		Limits: v1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(int64(gpus), resource.DecimalSI),
		},
	}
}

func inferenceSubGroups(topologyName string) []subGroupSpec {
	return []subGroupSpec{
		{
			name:      "prefill",
			pods:      inferencePrefillPods,
			resources: FullNodeGPURequirement,
			topology: &v2alpha2.TopologyConstraint{
				RequiredTopologyLevel: topologyRackLabel,
				Topology:              topologyName,
			},
		},
		{
			name:      "decode",
			pods:      inferenceDecodePods,
			resources: gpuRequirement(inferenceDecodeGPUs),
			topology: &v2alpha2.TopologyConstraint{
				PreferredTopologyLevel: topologyBlockLabel,
				Topology:               topologyName,
			},
		},
		{name: "frontend", pods: inferenceFrontendPods, resources: cpuOnlyRequirement},
	}
}

func createInferenceDeployments(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	deployments int, topologyName string,
) map[string]string {
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}
	podGroupTopology := &v2alpha2.TopologyConstraint{
		RequiredTopologyLevel: topologyZoneLabel,
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

			deploymentName := "inference-" + utils.GenerateRandomK8sName(10)
			deploymentLabels := map[string]string{
				distributedJobBatchLabel: batchLabels[distributedJobBatchLabel],
				inferenceDeploymentLabel: deploymentName,
			}
			_, _, err := createSubGroupPodGroupForKwok(
				ctx, testCtx, testQueue, deploymentName, podGroupTopology,
				inferenceSubGroups(topologyName), deploymentLabels,
			)
			if err != nil {
				lock.Lock()
				defer lock.Unlock()
				creationError = errors.Join(creationError, err)
			}
		}()
	}
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create inference deployments")
	return batchLabels
}

func disaggregatedInferenceAllocate(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	totalNodes int, topologyName string,
) map[string]string {
	deployments, err := inferenceDeploymentCount(totalNodes)
	Expect(err).NotTo(HaveOccurred())
	expectedPods := deployments * inferencePodsPerDeployment()

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	batchLabels := createInferenceDeployments(ctx, testCtx, testQueue, deployments, topologyName)
	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)
	endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, expectedPods)
	placement := validateInferencePlacement(ctx, testCtx, testQueue, batchLabels, deployments)

	Expect(writeTestResults("Disaggregated inference allocation", true, map[string]interface{}{
		"nodes":                             totalNodes,
		"deployments":                       deployments,
		"pods":                              expectedPods,
		"pods_per_deployment":               inferencePodsPerDeployment(),
		"duration_seconds":                  endTime.Sub(startTime).Seconds(),
		"decode_block_spread_by_deployment": placement,
	})).To(Succeed())
	return batchLabels
}

func disaggregatedInferenceReclaim(
	ctx context.Context, testCtx *testcontext.TestContext, victimQueue, reclaimQueue *v2.Queue,
	totalNodes int, topologyName string, victimBatchLabels map[string]string,
) {
	victimsBefore, victimGPUsBefore := scheduledBatchResources(ctx, testCtx, victimQueue, victimBatchLabels)
	startTime := time.Now()
	batchLabels := createInferenceDeployments(ctx, testCtx, reclaimQueue, 1, topologyName)
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, inferencePodsPerDeployment())
	placement := validateInferencePlacement(ctx, testCtx, reclaimQueue, batchLabels, 1)
	victimsAfter, victimGPUsAfter := scheduledBatchResources(ctx, testCtx, victimQueue, victimBatchLabels)

	reclaimerGPUs := inferencePrefillPods*gpusPerNode + inferenceDecodePods*inferenceDecodeGPUs
	Expect(victimGPUsBefore-victimGPUsAfter).To(BeNumerically(">=", reclaimerGPUs),
		"victim workload did not release enough GPU capacity")
	Expect(writeTestResults("Disaggregated inference reclaim", true, map[string]interface{}{
		"nodes":                             totalNodes,
		"pods":                              inferencePodsPerDeployment(),
		"victim_scheduled_pods_before":      victimsBefore,
		"victim_scheduled_pods_after":       victimsAfter,
		"victim_scheduled_gpus_before":      victimGPUsBefore,
		"victim_scheduled_gpus_after":       victimGPUsAfter,
		"duration_seconds":                  endTime.Sub(startTime).Seconds(),
		"decode_block_spread_by_deployment": placement,
	})).To(Succeed())
}

func validateInferencePlacement(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	batchLabels map[string]string, expectedDeployments int,
) map[string]int {
	pods := listBatchPods(ctx, testCtx, testQueue, batchLabels)
	nodes := listNodes(ctx, testCtx)
	byDeployment := map[string][]v1.Pod{}
	for _, pod := range pods {
		deployment := pod.Labels[inferenceDeploymentLabel]
		Expect(deployment).NotTo(BeEmpty(), "pod %s is missing deployment label", pod.Name)
		byDeployment[deployment] = append(byDeployment[deployment], pod)
	}
	Expect(byDeployment).To(HaveLen(expectedDeployments))

	decodeBlockSpread := make(map[string]int, expectedDeployments)
	for deployment, deploymentPods := range byDeployment {
		Expect(deploymentPods).To(HaveLen(inferencePodsPerDeployment()))
		zones, err := domainPathsForPods(deploymentPods, nodes, []string{topologyZoneLabel})
		Expect(err).NotTo(HaveOccurred())
		Expect(zones).To(HaveLen(1), "deployment %s must occupy one zone", deployment)

		prefillPods := podsInSubGroup(deploymentPods, "prefill")
		Expect(prefillPods).To(HaveLen(inferencePrefillPods))
		racks, err := domainPathsForPods(
			prefillPods, nodes, []string{topologyZoneLabel, topologyBlockLabel, topologyRackLabel})
		Expect(err).NotTo(HaveOccurred())
		Expect(racks).To(HaveLen(1), "deployment %s prefill pods must occupy one rack", deployment)

		decodePods := podsInSubGroup(deploymentPods, "decode")
		Expect(decodePods).To(HaveLen(inferenceDecodePods))
		blocks, err := domainPathsForPods(decodePods, nodes, []string{topologyZoneLabel, topologyBlockLabel})
		Expect(err).NotTo(HaveOccurred())
		decodeBlockSpread[deployment] = len(blocks)
	}
	return decodeBlockSpread
}

func elasticJobReclaim(
	ctx context.Context, testCtx *testcontext.TestContext,
	victimQueue, reclaimQueue *v2.Queue, numberOfNodes int,
) {
	victimMinMember, reclaimerPods, err := splitClusterForElasticReclaim(numberOfNodes)
	Expect(err).NotTo(HaveOccurred())
	victim, err := createElasticPodGroupForKwok(ctx, testCtx, victimQueue, numberOfNodes, victimMinMember)
	Expect(err).NotTo(HaveOccurred())
	victimNamespace := queue.GetConnectedNamespaceToQueue(victimQueue)
	waitutils.ForPodsScheduled(ctx, testCtx.ControllerClient, victimNamespace, victim.pods)

	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}
	startTime := time.Now()
	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	// Independent reclaimers make each reclaim decision observe the victim's updated elastic floor.
	for range reclaimerPods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := createJobObjectForKwok(ctx, testCtx, reclaimQueue, FullNodeGPURequirement, batchLabels)
			if err != nil {
				lock.Lock()
				defer lock.Unlock()
				creationError = errors.Join(creationError, err)
			}
		}()
	}
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "Failed to create elastic reclaimers")
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, reclaimerPods)
	waitForElasticVictimState(ctx, testCtx, victimNamespace, victim, victimMinMember)

	Expect(writeTestResults("Elastic distributed job reclaim", true, map[string]interface{}{
		"nodes":                        numberOfNodes,
		"victim_pods":                  numberOfNodes,
		"victim_min_member":            victimMinMember,
		"victim_scheduled_pods_before": numberOfNodes,
		"victim_scheduled_pods_after":  victimMinMember,
		"reclaimer_pods":               reclaimerPods,
		"duration_seconds":             endTime.Sub(startTime).Seconds(),
	})).To(Succeed())
}

type elasticVictim struct {
	podGroupName string
	pods         []*v1.Pod
}

func createElasticPodGroupForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, victimQueue *v2.Queue,
	podCount, minMember int,
) (elasticVictim, error) {
	namespace := queue.GetConnectedNamespaceToQueue(victimQueue)
	podGroupName := "elastic-victim-" + utils.GenerateRandomK8sName(10)
	podGroup := pod_group.Create(namespace, podGroupName, victimQueue.Name)
	podGroup.Spec.MinMember = ptr.To(int32(minMember))
	if err := rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, podGroup); err != nil {
		return elasticVictim{}, err
	}

	pods := make([]*v1.Pod, 0, podCount)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for range podCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pod := rd.CreatePodWithPodGroupReference(victimQueue, podGroupName, FullNodeGPURequirement)
			addKWOKTaintsAndAffinity(&pod.Spec)
			err := rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, pod)
			lock.Lock()
			defer lock.Unlock()
			if err != nil {
				creationError = errors.Join(creationError, err)
				return
			}
			pods = append(pods, pod)
		}()
	}
	wg.Wait()
	return elasticVictim{podGroupName: podGroupName, pods: pods}, creationError
}

func waitForElasticVictimState(
	ctx context.Context, testCtx *testcontext.TestContext, namespace string,
	victim elasticVictim, expectedScheduledPods int,
) {
	Eventually(func(g Gomega) {
		pods := &v1.PodList{}
		g.Expect(testCtx.ControllerClient.List(ctx, pods,
			runtimeClient.InNamespace(namespace),
			runtimeClient.MatchingLabels{pod_group.PodGroupNameAnnotation: victim.podGroupName},
		)).To(Succeed())
		scheduledPods := 0
		for i := range pods.Items {
			if rd.IsPodScheduled(&pods.Items[i]) {
				scheduledPods++
			}
		}
		g.Expect(scheduledPods).To(Equal(expectedScheduledPods))
	}, elasticReclaimVerificationTimeout, podsPollIntervalSeconds*time.Second).Should(Succeed())
}

func heroJobReclaim(
	ctx context.Context, testCtx *testcontext.TestContext, victimQueue, reclaimQueue *v2.Queue,
	heroPods int, topologyName string,
) {
	victimsBefore := countScheduledQueuePods(ctx, testCtx, victimQueue)
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}
	startTime := time.Now()
	_, err := submitDistributedJobForKwok(
		ctx, testCtx, reclaimQueue, FullNodeGPURequirement, heroPods,
		batchLabels, batchLabels, &v2alpha2.TopologyConstraint{
			RequiredTopologyLevel: topologyZoneLabel,
			Topology:              topologyName,
		},
	)
	Expect(err).NotTo(HaveOccurred())
	endTime := waitForBatchToSchedule(ctx, testCtx, reclaimQueue, batchLabels, heroPods)

	heroPodList := listBatchPods(ctx, testCtx, reclaimQueue, batchLabels)
	zones, err := domainPathsForPods(heroPodList, listNodes(ctx, testCtx), []string{topologyZoneLabel})
	Expect(err).NotTo(HaveOccurred())
	Expect(zones).To(HaveLen(1), "hero pods must occupy one zone")
	victimsAfter := countScheduledQueuePods(ctx, testCtx, victimQueue)
	Expect(victimsBefore-victimsAfter).To(BeNumerically(">=", heroPods*gpusPerNode),
		"insufficient victim GPU capacity was reclaimed")

	Expect(writeTestResults("Zone-constrained hero job reclaim", true, map[string]interface{}{
		"nodes":                        heroPods * 2,
		"pods":                         heroPods,
		"total_requested_gpus":         heroPods * gpusPerNode,
		"required_topology_level":      topologyZoneLabel,
		"topology_domain":              zones[0],
		"victim_scheduled_pods_before": victimsBefore,
		"victim_scheduled_pods_after":  victimsAfter,
		"duration_seconds":             endTime.Sub(startTime).Seconds(),
	})).To(Succeed())
}

func waitForBatchToSchedule(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	batchLabels map[string]string, expectedPods int,
) time.Time {
	var podsList v1.PodList
	Eventually(func(g Gomega) {
		g.Expect(testCtx.ControllerClient.List(ctx, &podsList,
			runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
			runtimeClient.MatchingLabels(batchLabels),
		)).To(Succeed())
		g.Expect(podsList.Items).To(HaveLen(expectedPods))
		for i := range podsList.Items {
			g.Expect(rd.IsPodScheduled(&podsList.Items[i])).To(BeTrue(), "pod %s is not scheduled", podsList.Items[i].Name)
		}
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())

	var lastScheduledTime time.Time
	for i := range podsList.Items {
		scheduledTime, err := getPodScheduledTime(&podsList.Items[i])
		Expect(err).NotTo(HaveOccurred(), "pod %s has no scheduled timestamp", podsList.Items[i].Name)
		if lastScheduledTime.Before(scheduledTime) {
			lastScheduledTime = scheduledTime
		}
	}
	return lastScheduledTime
}

func scheduledBatchResources(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, batchLabels map[string]string,
) (int, int64) {
	pods := listBatchPods(ctx, testCtx, testQueue, batchLabels)
	return scheduledResources(pods)
}

func countScheduledQueuePods(ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue) int {
	pods := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
		runtimeClient.MatchingLabels{testconfig.GetConfig().QueueLabelKey: testQueue.Name},
	)).To(Succeed())
	return countScheduledPods(pods.Items)
}

func countScheduledPods(pods []v1.Pod) int {
	scheduled := 0
	for i := range pods {
		if rd.IsPodScheduled(&pods[i]) {
			scheduled++
		}
	}
	return scheduled
}

func scheduledResources(pods []v1.Pod) (int, int64) {
	scheduledPods := 0
	var scheduledGPUs int64
	for i := range pods {
		if !rd.IsPodScheduled(&pods[i]) {
			continue
		}
		scheduledPods++
		gpuQuantity := pods[i].Spec.Containers[0].Resources.Limits[constants.NvidiaGpuResource]
		scheduledGPUs += gpuQuantity.Value()
	}
	return scheduledPods, scheduledGPUs
}

func listBatchPods(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, batchLabels map[string]string,
) []v1.Pod {
	pods := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(queue.GetConnectedNamespaceToQueue(testQueue)),
		runtimeClient.MatchingLabels(batchLabels),
	)).To(Succeed())
	return pods.Items
}

func listNodes(ctx context.Context, testCtx *testcontext.TestContext) []v1.Node {
	nodes := &v1.NodeList{}
	Expect(testCtx.ControllerClient.List(ctx, nodes)).To(Succeed())
	return nodes.Items
}

func podsInSubGroup(pods []v1.Pod, subGroup string) []v1.Pod {
	result := make([]v1.Pod, 0)
	for i := range pods {
		if pods[i].Labels[constants.SubGroupLabelKey] == subGroup {
			result = append(result, pods[i])
		}
	}
	return result
}

func domainPathsForPods(pods []v1.Pod, nodes []v1.Node, nodeLabels []string) ([]string, error) {
	if len(pods) == 0 {
		return nil, errors.New("no pods were provided for topology validation")
	}
	if len(nodeLabels) == 0 {
		return nil, errors.New("no topology domains were provided for validation")
	}
	nodesByName := make(map[string]v1.Node, len(nodes))
	for i := range nodes {
		nodesByName[nodes[i].Name] = nodes[i]
	}

	domains := map[string]struct{}{}
	for i := range pods {
		pod := &pods[i]
		if pod.Spec.NodeName == "" {
			return nil, fmt.Errorf("pod %s is not assigned to a node", pod.Name)
		}
		node, found := nodesByName[pod.Spec.NodeName]
		if !found {
			return nil, fmt.Errorf("node %s for pod %s was not found", pod.Spec.NodeName, pod.Name)
		}
		path := make([]string, len(nodeLabels))
		for j, label := range nodeLabels {
			value := node.Labels[label]
			if value == "" {
				return nil, fmt.Errorf("node %s is missing topology label %s", node.Name, label)
			}
			path[j] = label + "=" + value
		}
		domains[strings.Join(path, "/")] = struct{}{}
	}

	result := make([]string, 0, len(domains))
	for domain := range domains {
		result = append(result, domain)
	}
	sort.Strings(result)
	return result, nil
}
