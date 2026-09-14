// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	e2econstant "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAPreemptSpecs asserts NUMA constraints are honored when a higher-priority pod preempts:
// preemption frees a zone the preemptor can then occupy.
func DescribeNUMAPreemptSpecs() bool {
	return Describe("NUMA preempt", Ordered, Serial, Label("numa"), func() {
		var (
			testCtx      *testcontext.TestContext
			lowPriority  string
			highPriority string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())

			lowPriority = utils.GenerateRandomK8sName(10)
			highPriority = utils.GenerateRandomK8sName(10)
			_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().Create(
				ctx, rd.CreatePriorityClass(lowPriority, e2econstant.NonPreemptiblePriorityThreshold/4), metav1.CreateOptions{})
			Expect(err).To(Succeed())
			_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().Create(
				ctx, rd.CreatePriorityClass(highPriority, e2econstant.NonPreemptiblePriorityThreshold/2), metav1.CreateOptions{})
			Expect(err).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("high-priority pod preempts a single victim to open a NUMA zone", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := numautil.RequireNodes(ctx, testCtx.ControllerClient,
				numautil.Requirement{Modeled: true, MinZones: 2, ZoneGPUs: 1})[0]

			zoneGPUs, ok := node.OneZoneGPUs()
			if !ok {
				Skip("no GPU-bearing zone on the discovered node")
			}
			// Need >=2 GPUs per zone: after freeing one GPU per zone a zone still holds a victim, and the
			// two-GPU preemptor needs a zone that can hold two once a victim is evicted.
			if zoneGPUs < 2 {
				Skip("need at least two GPUs per zone")
			}
			const preemptorGPUs = 2
			total := int(node.TotalGPUs())

			parent, child := gpuQueues(float64(total))
			testCtx.InitQueues([]*v2.Queue{child, parent})

			// Fill every GPU with a single-GPU low-priority pod
			fillers := make([]*v1.Pod, 0, total)
			for range total {
				pod := node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), 1))
				pod.Spec.PriorityClassName = lowPriority
				fillers = append(fillers, createPod(ctx, testCtx, pod))
			}
			for _, pod := range fillers {
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			}

			// Delete one filler per zone, leaving exactly one free GPU in each zone.
			victimByZone := map[string]*v1.Pod{}
			for _, pod := range fillers {
				zones := awaitObservedZones(ctx, testCtx, pod)
				if len(zones) != 1 {
					continue
				}
				if _, seen := victimByZone[zones[0].Zone]; !seen {
					victimByZone[zones[0].Zone] = pod
				}
			}
			Expect(len(victimByZone)).To(BeNumerically(">=", 2), "fillers should cover at least two zones")

			freed := map[string]bool{}
			for _, pod := range victimByZone {
				Expect(testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Delete(
					ctx, pod.Name, metav1.DeleteOptions{})).To(Succeed())
				freed[pod.Name] = true
			}
			remaining := make([]*v1.Pod, 0, total)
			for _, pod := range fillers {
				if !freed[pod.Name] {
					remaining = append(remaining, pod)
				}
			}
			// Wait for the freed GPUs to be released before submitting the preemptor.
			for _, pod := range victimByZone {
				p := pod
				Eventually(func(g Gomega) {
					_, err := testCtx.KubeClientset.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				}, reconcileTimeout, reconcilePoll).Should(Succeed())
			}

			highPod := node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), preemptorGPUs))
			highPod.Spec.PriorityClassName = highPriority
			created := createPod(ctx, testCtx, highPod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, created)

			// Assert on the preemption: the preemptor should have evicted exactly one more victim to fit in
			// a zone.
			Eventually(func(g Gomega) {
				evicted := 0
				for _, pod := range remaining {
					_, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
					if apierrors.IsNotFound(err) {
						evicted++
					}
				}
				g.Expect(evicted).To(Equal(1), "exactly one additional victim should be preempted")
			}, reconcileTimeout, reconcilePoll).Should(Succeed())
		})
	})
}
