/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package preempt

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait"
	poll "k8s.io/apimachinery/pkg/util/wait"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Preemption with Max Pods Limit", Ordered, func() {
	var (
		testCtx                         *testcontext.TestContext
		lowPreemptiblePriorityClass     string
		highPreemptiblePriorityClass    string
		lowNonPreemptiblePriorityClass  string
		highNonPreemptiblePriorityClass string
		targetNode                      string
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)
		capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset, &capacity.ResourceList{
			Gpu:      resource.MustParse("1"),
			Cpu:      resource.MustParse("100m"),
			PodCount: 1,
		})

		// Get a node with GPU for testing
		nodes, err := testCtx.KubeClientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).To(Succeed())
		for _, node := range nodes.Items {
			// Find a node with GPU
			if gpuCount, ok := node.Status.Allocatable[constants.GpuResource]; ok && gpuCount.Value() > 0 {
				targetNode = node.Name
				break
			}
		}
		if targetNode == "" {
			Skip("No node with GPU found")
		}

		parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
		testQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
		testCtx.InitQueues([]*v2.Queue{testQueue, parentQueue})

		lowPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		lowPreemptiblePriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowPreemptiblePriorityClass, lowPreemptiblePriorityValue),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		highPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highPreemptiblePriorityClass, lowPreemptiblePriorityValue+1),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		lowNonPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		lowNonPreemptiblePriorityValue := utils.RandomIntBetween(constant.NonPreemptiblePriorityThreshold,
			constant.NonPreemptiblePriorityThreshold*2)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(lowNonPreemptiblePriorityClass, lowNonPreemptiblePriorityValue),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())

		highNonPreemptiblePriorityClass = utils.GenerateRandomK8sName(10)
		_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
			Create(ctx, rd.CreatePriorityClass(highNonPreemptiblePriorityClass, lowNonPreemptiblePriorityValue+1),
				metav1.CreateOptions{})
		Expect(err).To(Succeed())
	})

	AfterAll(func(ctx context.Context) {
		err := rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)
		Expect(err).To(Succeed())
		testCtx.ClusterCleanup(ctx)
	})

	AfterEach(func(ctx context.Context) {
		testCtx.TestContextCleanup(ctx)
	})

	It("Simple case: preempt on node at max pods", func(ctx context.Context) {
		// Get node's max pod capacity
		node, err := testCtx.KubeClientset.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
		Expect(err).To(Succeed())
		maxPods := int(node.Status.Allocatable.Pods().Value())

		// Fill node to max capacity with low-priority CPU pods
		_, _, err = fillers.FillAllNodesWithJobs(ctx, testCtx, testCtx.Queues[0],
			v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("10m"),
				},
			},
			nil, nil, lowPreemptiblePriorityClass, targetNode)
		Expect(err).To(Succeed())

		// Verify node is at max pods
		node, err = testCtx.KubeClientset.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
		Expect(err).To(Succeed())
		podList, err := testCtx.KubeClientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", targetNode),
		})
		Expect(err).To(Succeed())
		currentPods := len(podList.Items)
		maxPods = int(node.Status.Allocatable.Pods().Value())
		Expect(currentPods).To(Equal(maxPods), "Node should be at max pod capacity")

		// Create high-priority pod that should preempt one low-priority pod
		highPriorityPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("10m"),
			},
		})
		highPriorityPod.Spec.PriorityClassName = highPreemptiblePriorityClass
		highPriorityPod.Spec.NodeSelector = map[string]string{
			constant.NodeNamePodLabelName: targetNode,
		}

		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, highPriorityPod)
		Expect(err).To(Succeed())

		// Wait for preemption and scheduling
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, highPriorityPod)

		// Verify high-priority pod is scheduled on target node
		scheduledPod, err := testCtx.KubeClientset.CoreV1().Pods(highPriorityPod.Namespace).
			Get(ctx, highPriorityPod.Name, metav1.GetOptions{})
		Expect(err).To(Succeed())
		Expect(scheduledPod.Spec.NodeName).To(Equal(targetNode))
	})

	It("node at maxPods-1, fraction pod cannot allocate", Label(labels.ReservationPod), func(ctx context.Context) {
		// Get node's max pod capacity
		node, err := testCtx.KubeClientset.CoreV1().Nodes().Get(ctx, targetNode, metav1.GetOptions{})
		Expect(err).To(Succeed())
		maxPods := int(node.Status.Allocatable.Pods().Value())

		// Fill node to max capacity with low-priority CPU pods
		_, _, err = fillers.FillAllNodesWithJobs(ctx, testCtx, testCtx.Queues[0],
			v1.ResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU: resource.MustParse("10m"),
				},
			},
			nil, nil, lowPreemptiblePriorityClass, targetNode)
		Expect(err).To(Succeed())

		// Verify node is at max pods
		var podsOnNode []v1.Pod
		Eventually(func(g Gomega) {
			podsOnNode, err = getRunningPodsOnNode(ctx, testCtx, targetNode)
			g.Expect(err).To(Succeed())
			currentPods := len(podsOnNode)
			maxPods = int(node.Status.Allocatable.Pods().Value())
			g.Expect(currentPods).To(Equal(maxPods), "Node should be at max pod capacity")
		}, 1*time.Minute, 1*time.Second).Should(Succeed(), fmt.Sprintf("Failed to wait for %d pods on node %s", maxPods, targetNode))

		// delete one e2e pod
		var deletedPodName string
		namespace := queue.GetConnectedNamespaceToQueue(testCtx.Queues[0])
		for _, pod := range podsOnNode {
			if pod.Labels[constant.AppLabelName] != "engine-e2e" {
				continue
			}
			if pod.Status.Phase != v1.PodRunning {
				continue
			}
			if pod.Spec.NodeName != targetNode {
				continue
			}
			deletedPodName = pod.Name

			err = testCtx.KubeClientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			Expect(err).To(Succeed())

			break
		}

		// Wait for pod to be deleted
		Eventually(func(g Gomega) {
			p, err := testCtx.KubeClientset.CoreV1().Pods(namespace).Get(ctx, deletedPodName, metav1.GetOptions{})
			if p != nil {
				GinkgoWriter.Printf("pod phase: %s, deletionTimestamp: %+v\n", p.Status.Phase, p.DeletionTimestamp)
			}
			if err != nil {
				GinkgoWriter.Printf("error: %+v\n", err)
			}
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}, 1*time.Minute, 1*time.Second).Should(Succeed())

		// Wait for node to be at maxPods-1
		err = poll.PollUntilContextTimeout(ctx, 1*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			podsOnNode, err = getRunningPodsOnNode(ctx, testCtx, targetNode)
			if err != nil {
				return false, err
			}
			maxPods = int(node.Status.Allocatable.Pods().Value())
			return len(podsOnNode) == maxPods-1, nil
		})
		Expect(err).To(Succeed(), fmt.Sprintf("Failed to wait for node to be at %d pods, current pods: %d", maxPods-1, len(podsOnNode)))

		// Create a fractional GPU pod (will need 2 pods: task + reservation)
		// This should fail because 109 + 2 = 111 > 110
		fractionPod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
		fractionPod.Annotations = map[string]string{
			constants.GpuFraction: "0.5",
		}
		fractionPod.Spec.PriorityClassName = lowPreemptiblePriorityClass
		fractionPod.Spec.NodeSelector = map[string]string{
			constant.NodeNamePodLabelName: targetNode,
		}

		_, err = rd.CreatePod(ctx, testCtx.KubeClientset, fractionPod)
		Expect(err).To(Succeed())

		// Wait and verify pod remains unschedulable
		wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, fractionPod)

		wait.WaitForEventInNamespaceAndPod(ctx, testCtx.ControllerClient, fractionPod.Namespace, fractionPod.Name, func(event *v1.Event) bool {
			return event.Reason == "Unschedulable" && (strings.Contains(event.Message, "pod number exceeded") || strings.Contains(event.Message, "max pods"))
		})
	})
})

func getRunningPodsOnNode(ctx context.Context, testCtx *testcontext.TestContext, nodeName string) ([]v1.Pod, error) {
	var podList v1.PodList
	err := testCtx.ControllerClient.List(ctx, &podList, &runtimeClient.ListOptions{
		Raw: &metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", nodeName),
		},
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}
