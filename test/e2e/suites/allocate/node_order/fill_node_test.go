/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package node_order

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeFillNodeSpecs() bool {
	return Describe("Fill node with fractional GPUs", Label(labels.Operated, labels.ReservationPod), Ordered, func() {
		var (
			testCtx  *testcontext.TestContext
			gpuNodes []*v1.Node
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)

			gpuNodes = findDevicePluginGPUNodes(ctx, testCtx)
			if len(gpuNodes) < 2 {
				Skip(fmt.Sprintf("Need at least 2 device-plugin GPU nodes, found %d", len(gpuNodes)))
			}

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, SpreadingPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to set spread placement strategy: %v", err))
			}
			if err := feature_flags.SetPluginEnabled(ctx, testCtx, "gpupack", true); err != nil {
				Fail(fmt.Sprintf("Failed to enable gpupack plugin: %v", err))
			}
			if err := feature_flags.SetPluginEnabled(ctx, testCtx, "gpuspread", false); err != nil {
				Fail(fmt.Sprintf("Failed to disable gpuspread plugin: %v", err))
			}
		})

		AfterAll(func(ctx context.Context) {
			if err := feature_flags.UnsetPlugin(ctx, testCtx, "gpupack"); err != nil {
				Fail(fmt.Sprintf("Failed to unset gpupack plugin: %v", err))
			}
			if err := feature_flags.UnsetPlugin(ctx, testCtx, "gpuspread"); err != nil {
				Fail(fmt.Sprintf("Failed to unset gpuspread plugin: %v", err))
			}
			if err := feature_flags.SetPlacementStrategy(ctx, testCtx, DefaultPluginName); err != nil {
				Fail(fmt.Sprintf("Failed to restore default placement strategy: %v", err))
			}
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("should pack fractional GPU pods onto one GPU per node under spread strategy", func(ctx context.Context) {
			numGPUNodes := len(gpuNodes)
			numPods := numGPUNodes * 2

			pods := make([]*v1.Pod, numPods)
			for i := range numPods {
				pods[i] = rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pods[i].Annotations = map[string]string{
					constants.GpuFraction: "0.5",
				}
				var err error
				pods[i], err = rd.CreatePod(ctx, testCtx.KubeClientset, pods[i])
				Expect(err).NotTo(HaveOccurred())
			}

			namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
			wait.ForPodsReady(ctx, testCtx.ControllerClient, namespace, pods)

			reservationPods, err := testCtx.KubeClientset.CoreV1().
				Pods(constant.KaiReservationNamespace).
				List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			reservationsByNode := map[string]int{}
			for _, pod := range reservationPods.Items {
				if pod.Spec.NodeName != "" {
					reservationsByNode[pod.Spec.NodeName]++
				}
			}

			for _, node := range gpuNodes {
				Expect(reservationsByNode[node.Name]).To(Equal(1),
					fmt.Sprintf("Expected exactly 1 reservation pod on node %s, got %d",
						node.Name, reservationsByNode[node.Name]))
			}
		})
	})
}

func findDevicePluginGPUNodes(ctx context.Context, testCtx *testcontext.TestContext) []*v1.Node {
	allNodes := v1.NodeList{}
	Expect(testCtx.ControllerClient.List(ctx, &allNodes)).To(Succeed())

	var gpuNodes []*v1.Node
	for i, node := range allNodes.Items {
		if len(node.Spec.Taints) > 0 {
			continue
		}
		gpuResource, found := node.Status.Capacity[constants.NvidiaGpuResource]
		if found && gpuResource.CmpInt64(0) > 0 {
			gpuNodes = append(gpuNodes, &allNodes.Items[i])
		}
	}
	return gpuNodes
}
