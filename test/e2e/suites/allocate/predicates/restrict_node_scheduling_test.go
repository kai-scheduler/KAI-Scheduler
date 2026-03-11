/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package predicates

import (
	"context"
	"fmt"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	cpuWorkerLabelName = "node-role.kubernetes.io/cpu-worker"
	gpuWorkerLabelName = "node-role.kubernetes.io/gpu-worker"
)

var _ = Describe("Restrict node scheduling", Label(labels.Operated), Ordered, func() {
	var (
		testCtx *testContext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testContext.GetConnectivity(ctx, Default)
		err := feature_flags.SetRestrictNodeScheduling(ptr.To(true), testCtx, ctx)
		Expect(err).NotTo(HaveOccurred())

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
	})

	AfterAll(func(ctx context.Context) {
		err := feature_flags.SetRestrictNodeScheduling(ptr.To(false), testCtx, ctx)
		Expect(err).NotTo(HaveOccurred())
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	Context("GPU worker node", func() {
		var node *corev1.Node

		BeforeAll(func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("1"),
					PodCount: 1,
				},
			)
			node = setupGpuNode(testCtx, ctx)
			if node == nil {
				Skip("Could not find GPU node")
			}
		})

		AfterAll(func(ctx context.Context) {
			if node != nil {
				err := unlabelWorkers(ctx, testCtx.KubeClientset, node)
				Expect(err).To(Succeed())
			}
		})

		It("Scheduling GPU pod", func(ctx context.Context) {
			gpuPod := rd.CreatePodObject(testCtx.Queues[0], corev1.ResourceRequirements{
				Limits: map[corev1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})

			gpuPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, gpuPod)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, gpuPod)
		})

		It("Not scheduling cpu pod", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Cpu:      resource.MustParse("100m"),
					PodCount: 1,
				},
			)

			cpuPod := rd.CreatePodObject(testCtx.Queues[0], corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
			})

			cpuPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, cpuPod)
			Expect(err).To(Succeed())

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, cpuPod)
		})
	})

	Context("CPU worker node", func() {
		var node *corev1.Node

		BeforeAll(func(ctx context.Context) {
			node = setupCpuNode(testCtx, ctx, resource.MustParse("100m"))
			if node == nil {
				Skip("Could not find CPU node")
			}
		})

		AfterAll(func(ctx context.Context) {
			err := unlabelWorkers(ctx, testCtx.KubeClientset, node)
			Expect(err).To(Succeed())
		})

		It("Scheduling cpu pod", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Cpu:      resource.MustParse("100m"),
					PodCount: 1,
				},
			)

			cpuPod := rd.CreatePodObject(testCtx.Queues[0], corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU: resource.MustParse("100m"),
				},
			})

			cpuPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, cpuPod)
			Expect(err).To(Succeed())

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, cpuPod)
		})

		It("Not scheduling GPU pod", func(ctx context.Context) {
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{
					Gpu:      resource.MustParse("1"),
					PodCount: 1,
				},
			)

			gpuPod := rd.CreatePodObject(testCtx.Queues[0], corev1.ResourceRequirements{
				Limits: map[corev1.ResourceName]resource.Quantity{
					constants.NvidiaGpuResource: resource.MustParse("1"),
				},
			})

			gpuPod, err := rd.CreatePod(ctx, testCtx.KubeClientset, gpuPod)
			Expect(err).To(Succeed())

			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, gpuPod)
		})
	})
})

func setupGpuNode(textCtx *testContext.TestContext, ctx context.Context) *corev1.Node {
	nodes, err := textCtx.KubeClientset.CoreV1().Nodes().
		List(ctx, v1.ListOptions{LabelSelector: constants.GpuCountLabel})
	Expect(err).NotTo(HaveOccurred(), "Failed to list nodes")
	nodes.Items = filterTaintedNodes(nodes.Items)
	if len(nodes.Items) == 0 {
		return nil
	}
	node := nodes.Items[0]

	err = rd.LabelNode(ctx, textCtx.KubeClientset, &node, gpuWorkerLabelName, "true")
	Expect(err).NotTo(HaveOccurred(), "Failed to label nodes")

	return &node
}

func setupCpuNode(textCtx *testContext.TestContext, ctx context.Context, availableCPU resource.Quantity) *corev1.Node {
	nodes, err := textCtx.KubeClientset.CoreV1().Nodes().
		List(ctx, v1.ListOptions{LabelSelector: fmt.Sprintf("!%s", gpuWorkerLabelName)})
	Expect(err).NotTo(HaveOccurred(), "Failed to list nodes")
	nodes.Items = filterTaintedNodes(nodes.Items)
	Expect(len(nodes.Items)).Should(BeNumerically(">", 0), "No non-gpu nodes")
	node := getNodeWithAvailableCPU(textCtx, ctx, availableCPU, nodes)
	if node == nil {
		return nil
	}

	err = rd.LabelNode(ctx, textCtx.KubeClientset, node, cpuWorkerLabelName, "true")
	Expect(err).NotTo(HaveOccurred(), "Failed to label nodes")

	return node
}

func getNodeWithAvailableCPU(testCtx *testContext.TestContext, ctx context.Context, availableCPU resource.Quantity, nodes *corev1.NodeList) *corev1.Node {
	for _, node := range nodes.Items {
		podsOnNode := &corev1.PodList{}
		err := testCtx.ControllerClient.List(ctx, podsOnNode, &client.ListOptions{
			FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": node.Name}),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to list pods on node")
		totalAvailableCPU := node.Status.Allocatable[corev1.ResourceCPU]
		for _, pod := range podsOnNode.Items {
			if slices.Contains([]corev1.PodPhase{corev1.PodPending, corev1.PodSucceeded, corev1.PodFailed}, pod.Status.Phase) {
				continue
			}
			for _, container := range pod.Spec.Containers {
				totalAvailableCPU.Sub(container.Resources.Requests[corev1.ResourceCPU])
			}
		}
		if totalAvailableCPU.Cmp(availableCPU) >= 0 {
			return &node
		}
	}
	return nil
}

func filterTaintedNodes(nodes []corev1.Node) []corev1.Node {
	var newNodes []corev1.Node
	for _, node := range nodes {
		if len(node.Spec.Taints) == 0 {
			newNodes = append(newNodes, node)
		}
	}
	return newNodes
}

func unlabelWorkers(ctx context.Context, k8sClient kubernetes.Interface, nodes ...*corev1.Node) error {
	var errs []error
	for _, node := range nodes {
		err := rd.UnLabelNode(ctx, k8sClient, node, cpuWorkerLabelName)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to unlabel node %s with label %s, error: %w",
				node.Name, cpuWorkerLabelName, err))
		}

		err = rd.UnLabelNode(ctx, k8sClient, node, gpuWorkerLabelName)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to unlabel node %s with label %s, error: %w",
				node.Name, gpuWorkerLabelName, err))
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("failed to remove labels from nodes, error: %s", errs)
	}

	return nil
}
