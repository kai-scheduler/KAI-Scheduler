/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package topology

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Topology", Ordered, func() {
	var (
		testCtx          *testcontext.TestContext
		gpuNodesNames    []string
		testTopologyData rd.TestTopologyData
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		// Set spreading strategy to try and increase the probability of
		//  out-of-topology allocation more common in case of a bug.
		if err := feature_flags.SetPlacementStrategy(ctx, testCtx, feature_flags.SpreadStrategy); err != nil {
			Fail(fmt.Sprintf("Failed to patch scheduler config with spreading plugin: %v", err))
		}
	})

	AfterAll(func(ctx context.Context) {
		if err := feature_flags.SetPlacementStrategy(ctx, testCtx, feature_flags.DefaultStrategy); err != nil {
			Fail(fmt.Sprintf("Failed to patch scheduler config with spreading plugin: %v", err))
		}

		testCtx.ClusterCleanup(ctx)
	})

	Context("Topology - 4 nodes", func() {
		const numNodesInTestTopology = 4

		BeforeEach(func(ctx context.Context) {
			testTopologyData, gpuNodesNames = rd.CreateRackZoneTopology(ctx, testCtx.KubeClientset, testCtx.KubeConfig, numNodesInTestTopology, 2)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanRackZoneTopology(ctx, testTopologyData, testCtx.KubeConfig)
			})

			rd.AssignNodesToTestTopology(ctx, testCtx.ControllerClient, gpuNodesNames, testTopologyData, numNodesInTestTopology, false)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanNodesFromTopology(ctx, testCtx.ControllerClient, testTopologyData)
			})
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("required only - rack level", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				RequiredTopologyLevel: rd.TestRackLabelKey,
				Topology:              "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): gpusPerNode,
			}

			pods := createDistributedWorkload(ctx, testCtx, 2, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			// Validate that all the pods have been scheduled to the same rack
			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledRacks := map[string][]string{}
			for _, pod := range podList.Items {
				podRack := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestRackLabelKey]
				scheduledRacks[podRack] = append(scheduledRacks[podRack], pod.Name)
			}

			Expect(len(scheduledRacks)).To(Equal(1), "Expected all pods scheduled to one rack, got %v", scheduledRacks)
		})

		It("preferred only - rack level", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				PreferredTopologyLevel: rd.TestRackLabelKey,
				Topology:               "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): gpusPerNode,
			}

			pods := createDistributedWorkload(ctx, testCtx, 2, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			// Validate that all the pods have been scheduled to the same rack
			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledRacks := map[string][]string{}
			for _, pod := range podList.Items {
				podRack := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestRackLabelKey]
				scheduledRacks[podRack] = append(scheduledRacks[podRack], pod.Name)
			}

			Expect(len(scheduledRacks)).To(Equal(1), "Expected all pods scheduled to one rack, got %v", scheduledRacks)
		})

		It("required rack and preferred node - all pods in a single node", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				RequiredTopologyLevel:  rd.TestRackLabelKey,
				PreferredTopologyLevel: rd.NodeNameLabelKey,
				Topology:               "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			halfGpusPerNode := int64(gpusPerNode.AsFloat64Slow() / 2)
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): *resource.NewQuantity(halfGpusPerNode, resource.DecimalSI),
			}

			pods := createDistributedWorkload(ctx, testCtx, 2, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			// Validate that all the pods have been scheduled to the same rack
			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledNodes := map[string][]string{}
			for _, pod := range podList.Items {
				scheduledNodes[pod.Spec.NodeName] = append(scheduledNodes[pod.Spec.NodeName], pod.Name)
			}

			Expect(len(scheduledNodes)).To(Equal(1), "Expected all pods scheduled to one node, got %v", scheduledNodes)
		})

		It("required rack and preferred node - all pods in a rack", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				RequiredTopologyLevel:  rd.TestRackLabelKey,
				PreferredTopologyLevel: rd.NodeNameLabelKey,
				Topology:               "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): gpusPerNode,
			}

			pods := createDistributedWorkload(ctx, testCtx, 2, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			// Validate that all the pods have been scheduled to the same rack
			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledNodes := map[string][]string{}
			scheduledRacks := map[string][]string{}
			for _, pod := range podList.Items {
				scheduledNodes[pod.Spec.NodeName] = append(scheduledNodes[pod.Spec.NodeName], pod.Name)
				podRack := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestRackLabelKey]
				scheduledRacks[podRack] = append(scheduledRacks[podRack], pod.Name)
			}

			Expect(len(scheduledNodes)).To(BeNumerically(">", 1), "Expected all pods scheduled to one more then one node, got %v", scheduledNodes)
			Expect(len(scheduledRacks)).To(Equal(1), "Expected all pods scheduled to the same rack, got %v", scheduledRacks)
		})

		It("required node - replacement of a subgroup member stays on the node of the running members", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			halfGpusPerNode := *resource.NewQuantity(int64(gpusPerNode.AsFloat64Slow()/2), resource.DecimalSI)
			podResource := v1.ResourceRequirements{
				Requests: v1.ResourceList{v1.ResourceName(constants.NvidiaGpuResource): halfGpusPerNode},
				Limits:   v1.ResourceList{v1.ResourceName(constants.NvidiaGpuResource): halfGpusPerNode},
			}

			pgName := utils.GenerateRandomK8sName(10)
			subGroupNodes := []pod_group.SubGroupNode{
				{Name: "master", MinMember: ptr.To(int32(1)), PodCount: 1},
				{Name: "worker", MinMember: ptr.To(int32(1)), PodCount: 1},
			}
			hierarchy := pod_group.BuildHierarchy(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName, subGroupNodes, podResource)
			podGroup := pod_group.Create(namespace, pgName, testCtx.Queues[0].Name)
			podGroup.Spec.MinMember = ptr.To(int32(2))
			podGroup.Spec.SubGroups = hierarchy.SubGroups
			podGroup.Spec.TopologyConstraint = v2alpha2.TopologyConstraint{
				RequiredTopologyLevel: rd.NodeNameLabelKey,
				Topology:              "e2e-topology-tree",
			}
			_, err := testCtx.KubeAiSchedClientset.SchedulingV2alpha2().PodGroups(namespace).Create(ctx,
				podGroup, metav1.CreateOptions{})
			Expect(err).To(Succeed())
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, hierarchy.AllPods)

			master, err := rd.GetPod(ctx, testCtx.KubeClientset, namespace, hierarchy.Pods["master"][0].Name)
			Expect(err).To(Succeed())

			err = testCtx.KubeClientset.CoreV1().Pods(namespace).Delete(ctx, hierarchy.Pods["worker"][0].Name,
				metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))})
			Expect(err).To(Succeed())

			// The blocker takes the capacity freed on the master's node, so only other nodes can fit a replacement
			blocker := rd.CreatePodObject(testCtx.Queues[0], podResource)
			blocker.Spec.NodeSelector = map[string]string{rd.NodeNameLabelKey: master.Spec.NodeName}
			blocker, err = rd.CreatePod(ctx, testCtx.KubeClientset, blocker)
			Expect(err).To(Succeed())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, blocker)

			replacement := pod_group.BuildHierarchy(ctx, testCtx.KubeClientset, testCtx.Queues[0], pgName,
				subGroupNodes[1:], podResource).AllPods[0]
			// Fail as soon as the replacement binds instead of waiting for an Unschedulable condition that never comes
			Eventually(func() bool {
				current, err := rd.GetPod(ctx, testCtx.KubeClientset, namespace, replacement.Name)
				Expect(err).To(Succeed())
				if current.Spec.NodeName != "" {
					Fail(fmt.Sprintf("Replacement worker was bound to node %s, expected it to stay unschedulable "+
						"since the running master is on node %s", current.Spec.NodeName, master.Spec.NodeName))
				}
				return rd.IsPodUnschedulable(current)
			}, "1m", "500ms").Should(BeTrue())
		})
	}, MustPassRepeatedly(3))

	Context("Topology - 8 nodes", func() {
		const numNodesInTestTopology = 8

		BeforeEach(func(ctx context.Context) {
			testTopologyData, gpuNodesNames = rd.CreateRackZoneTopology(ctx, testCtx.KubeClientset, testCtx.KubeConfig, numNodesInTestTopology, 4)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanRackZoneTopology(ctx, testTopologyData, testCtx.KubeConfig)
			})

			rd.AssignNodesToTestTopology(ctx, testCtx.ControllerClient, gpuNodesNames, testTopologyData, numNodesInTestTopology, true)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanNodesFromTopology(ctx, testCtx.ControllerClient, testTopologyData)
			})
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("required zone and preferred rack - all pods in a zone, packed into 2 racks", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				RequiredTopologyLevel:  rd.TestZoneLabelKey,
				PreferredTopologyLevel: rd.TestRackLabelKey,
				Topology:               "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): gpusPerNode,
			}

			pods := createDistributedWorkload(ctx, testCtx, 4, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			// Validate that all the pods have been scheduled to the same zone
			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledZones := map[string][]string{}
			scheduledRacks := map[string][]string{}
			for _, pod := range podList.Items {
				podZone := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestZoneLabelKey]
				scheduledZones[podZone] = append(scheduledZones[podZone], pod.Name)
				podRack := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestRackLabelKey]
				scheduledRacks[podRack] = append(scheduledRacks[podRack], pod.Name)
			}

			Expect(len(scheduledZones)).To(Equal(1), "Expected all pods scheduled to one zone, got %v", scheduledZones)
			Expect(len(scheduledRacks)).To(Equal(2), "Expected all pods scheduled to 2 racks, got %v", scheduledRacks)
		})
	})

	Context("Topology - alias resolution", func() {
		const numNodesInTestTopology = 4

		BeforeEach(func(ctx context.Context) {
			testTopologyData, gpuNodesNames = rd.CreateRackZoneTopology(ctx, testCtx.KubeClientset, testCtx.KubeConfig, numNodesInTestTopology, 2)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanRackZoneTopology(ctx, testTopologyData, testCtx.KubeConfig)
			})

			rd.AssignNodesToTestTopology(ctx, testCtx.ControllerClient, gpuNodesNames, testTopologyData, numNodesInTestTopology, false)
			DeferCleanup(func(ctx context.Context) {
				rd.CleanNodesFromTopology(ctx, testCtx.ControllerClient, testTopologyData)
			})
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("schedules a workload that references the rack level by alias", func(ctx context.Context) {
			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			topologyConstraint := v2alpha2.TopologyConstraint{
				RequiredTopologyLevel: rd.TestRackAlias, // alias resolves to rd.TestRackLabelKey
				Topology:              "e2e-topology-tree",
			}

			gpusPerNode := testTopologyData.TopologyNodes[gpuNodesNames[0]].
				Status.Allocatable[v1.ResourceName(constants.NvidiaGpuResource)]
			podResource := v1.ResourceList{
				v1.ResourceName(constants.NvidiaGpuResource): gpusPerNode,
			}

			pods := createDistributedWorkload(ctx, testCtx, 2, podResource, topologyConstraint)
			wait.ForPodsScheduled(ctx, testCtx.ControllerClient, namespace, pods)

			podList, err := testCtx.KubeClientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to list pods")

			scheduledRacks := map[string][]string{}
			for _, pod := range podList.Items {
				podRack := testTopologyData.TopologyNodes[pod.Spec.NodeName].Labels[rd.TestRackLabelKey]
				scheduledRacks[podRack] = append(scheduledRacks[podRack], pod.Name)
			}

			Expect(len(scheduledRacks)).To(Equal(1), "Expected all pods scheduled to one rack via alias, got %v", scheduledRacks)
		})
	})

	Context("Empty context to jump over ginkgo bug", func() {
		It("should not create test suite while ensuring that the test suite is executed", func(ctx context.Context) {
			Expect(true).To(BeTrue())
		})
	})
}, Ordered)

func createDistributedWorkload(ctx context.Context, testCtx *testcontext.TestContext,
	podCount int, podResource v1.ResourceList, topologyConstraint v2alpha2.TopologyConstraint) []*v1.Pod {
	result, err := rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, testCtx.Queues[0],
		rd.DistributedBatchJobOptions{
			Parallelism:        ptr.To(int32(podCount)),
			NamePrefix:         "distributed-" + utils.GenerateRandomK8sName(5) + "-",
			Resources:          v1.ResourceRequirements{Requests: podResource, Limits: podResource},
			TopologyConstraint: &topologyConstraint,
		})
	Expect(err).To(Succeed())
	return result.Pods
}
