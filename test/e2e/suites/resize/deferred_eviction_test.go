/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package resize

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const evictionTimeout = 3 * time.Minute

var _ = Describe("Deferred resize eviction", Ordered, func() {
	var (
		testCtx *testcontext.TestContext
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		skipIfNoResizeSupport(testCtx)
		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
			&capacity.ResourceList{
				Cpu:      resource.MustParse("2"),
				PodCount: 2,
			},
		)
		Expect(feature_flags.SetResizeEvictionAction(ctx, testCtx, true)).To(Succeed())
	})

	AfterAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		Expect(feature_flags.SetResizeEvictionAction(ctx, testCtx, false)).To(Succeed())
		Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
		testCtx.ClusterCleanup(ctx)
	})

	It("evicts a lower priority pod when a resize is deferred for node capacity", func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{childQueue, parentQueue})

		lowPriority := utils.GenerateRandomK8sName(10)
		_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowPriority, 30), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		highPriority := utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highPriority, 70), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		nodeName, idleCPUMillis := pickNodeWithMostIdleCPU(testCtx)
		if idleCPUMillis < 1000 {
			Skip(fmt.Sprintf("node %s has only %dm idle CPU, need at least 1000m", nodeName, idleCPUMillis))
		}

		// Victim takes 60% of the node's idle CPU, the resizing pod starts at 20% and
		// grows to 80%: the target only fits once the victim is gone.
		victimCPU := idleCPUMillis * 60 / 100
		resizerCPU := idleCPUMillis * 20 / 100
		targetCPU := idleCPUMillis * 80 / 100

		By(fmt.Sprintf("running a %dm victim and a %dm resizing pod on node %s", victimCPU, resizerCPU, nodeName))
		victim := runPodOnNode(ctx, testCtx, nodeName, victimCPU, lowPriority)
		resizer := runPodOnNode(ctx, testCtx, nodeName, resizerCPU, highPriority)

		By(fmt.Sprintf("resizing to %dm, beyond the node's remaining capacity", targetCPU))
		target := fmt.Sprintf("%dm", targetCPU)
		Expect(resizeCPU(ctx, testCtx, resizer, target)).To(Succeed())

		By("waiting for the kubelet to mark the resize Deferred")
		Eventually(func(g Gomega) {
			current := &v1.Pod{}
			g.Expect(testCtx.ControllerClient.Get(ctx,
				types.NamespacedName{Namespace: resizer.Namespace, Name: resizer.Name}, current)).To(Succeed())
			g.Expect(isResizeDeferred(current)).To(BeTrue())
		}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).Should(Succeed())

		By("waiting for the scheduler to evict the victim")
		Eventually(func(g Gomega) {
			pod := &v1.Pod{}
			err := testCtx.ControllerClient.Get(ctx,
				types.NamespacedName{Namespace: victim.Namespace, Name: victim.Name}, pod)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "victim pod should be evicted")
		}).WithContext(ctx).WithTimeout(evictionTimeout).WithPolling(resizePoll).Should(Succeed())

		By("waiting for the resize to be enacted")
		expectEnactedCPU(ctx, testCtx, resizer, target)
	})
})

func runPodOnNode(
	ctx context.Context, testCtx *testcontext.TestContext,
	nodeName string, cpuMillis int64, priorityClassName string,
) *v1.Pod {
	pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewMilliQuantity(cpuMillis, resource.DecimalSI),
			v1.ResourceMemory: resource.MustParse("128Mi"),
		},
	})
	pod.Spec.NodeSelector = map[string]string{constant.NodeNamePodLabelName: nodeName}
	pod.Spec.PriorityClassName = priorityClassName
	pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).NotTo(HaveOccurred(), "failed to create pod")
	wait.ForPodReady(ctx, testCtx.ControllerClient, pod)
	return pod
}

func pickNodeWithMostIdleCPU(testCtx *testcontext.TestContext) (string, int64) {
	idleResources, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
	Expect(err).NotTo(HaveOccurred(), "failed to get nodes idle resources")

	nodes, err := testCtx.KubeClientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to list nodes")

	bestNode := ""
	bestCPU := int64(0)
	for _, node := range nodes.Items {
		if hasNoScheduleTaint(&node) {
			continue
		}
		idle, found := idleResources[node.Name]
		if !found {
			continue
		}
		idleCPU := idle.Cpu.MilliValue()
		if idleCPU > bestCPU {
			bestNode = node.Name
			bestCPU = idleCPU
		}
	}
	Expect(bestNode).NotTo(BeEmpty(), "no schedulable node found")
	return bestNode, bestCPU
}

func hasNoScheduleTaint(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == v1.TaintEffectNoSchedule || taint.Effect == v1.TaintEffectNoExecute {
			return true
		}
	}
	return false
}

func isResizeDeferred(pod *v1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == v1.PodResizePending {
			return c.Status == v1.ConditionTrue && c.Reason == v1.PodReasonDeferred
		}
	}
	return false
}
