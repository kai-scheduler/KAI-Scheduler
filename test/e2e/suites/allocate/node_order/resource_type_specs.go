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
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
)

const draDriverName = "gpu.nvidia.com"

func DescribeResourceTypeSpecs() bool {
	return Describe("Resource Type Plugins", Ordered, func() {
		var (
			testCtx *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})
		})

		AfterAll(func(ctx context.Context) {
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		Context("CPU only jobs", func() {
			BeforeEach(func(ctx context.Context) {
				cpuNode := rd.FindNodeWithNoGPU(ctx, testCtx.ControllerClient, testCtx.KubeClientset)
				if cpuNode == nil {
					Skip("Failed to find node without GPUs")
				}
			})

			It("schedules CPU only jobs on CPU nodes", func(ctx context.Context) {
				pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
				pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
				Expect(err).To(Succeed())
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
				Expect(testCtx.ControllerClient.Get(
					ctx, runtimeClient.ObjectKeyFromObject(pod), pod)).To(Succeed())
				var node v1.Node
				Expect(testCtx.ControllerClient.Get(
					ctx,
					runtimeClient.ObjectKey{Name: pod.Spec.NodeName},
					&node,
				)).To(Succeed())

				Expect(specIsCPUOnlyNode(&node, testCtx.KubeClientset)).To(BeTrue(),
					fmt.Sprintf("node %s is not CPU only", node.Name))
			})
		})
	})
}

func specIsCPUOnlyNode(node *v1.Node, clientset kubernetes.Interface) bool {
	gpus, found := node.Status.Capacity[constants.NvidiaGpuResource]
	hasDevicePluginGPUs := found && gpus.CmpInt64(0) > 0
	if hasDevicePluginGPUs {
		return false
	}

	draDevicesByNode := capacity.ListDevicesByNode(clientset, draDriverName)
	draGPUs, found := draDevicesByNode[node.Name]
	return !found || draGPUs == 0
}
