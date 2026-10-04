// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAModesSpecs covers Topology Manager policies and Memory Manager group admission.
// The suite mutates the shard plugin config, so it runs Serial.
func DescribeNUMAModesSpecs() bool {
	return Describe("NUMA modes", Ordered, Serial, Label("numa", "nightly"), func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parent, child := gpuQueues(64)
			testCtx.InitQueues([]*v2.Queue{child, parent})
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("single-numa-node - request fitting one zone is scheduled", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode, ZoneGPUs: 1})
			gpus, ok := node.OneZoneGPUs()
			if !ok {
				Skip("no single-numa-node node exposes a GPU-bearing zone")
			}

			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), gpus)))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			expectGPUPlacement(ctx, testCtx, pod, 1, gpus)
		})

		It("single-numa-node - request spanning two zones stays pending", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode, MinZones: 2})
			gpus, ok := node.SpanTwoZonesGPUs()
			if !ok {
				Skip("no single-numa-node node can express a two-zone-spanning request")
			}

			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), gpus)))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)

			// Negative control: the same pod downsized to one zone schedules on the same class of node.
			fit, ok := node.OneZoneGPUs()
			if !ok {
				return
			}
			fitPod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), fit)))
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, fitPod)
		})

		It("restricted - two-zone request at matching minimal width is scheduled", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicyRestricted, MinZones: 2})
			gpus, gpusOK := node.SpanTwoZonesGPUs()
			cpu, cpuOK := node.TwoZoneCPU()
			memory, memOK := node.TwoZoneMemory()
			if !gpusOK || !cpuOK || !memOK {
				Skip("no restricted node where GPU, CPU and memory can all span two zones")
			}

			// GPU, CPU and memory all need width 2, so restricted has a common preferred mask (both NUMA
			// nodes). A small CPU or memory request would force width 1 and conflict with the GPU span.
			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUCPUPod(childQueue(testCtx), gpus, cpu, memory)))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
			expectGPUPlacement(ctx, testCtx, pod, 2, gpus)
		})

		It("restricted - request exceeding summed capacity stays pending", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicyRestricted, ZoneGPUs: 1})
			gpus := node.TotalGPUs() + 1

			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), gpus)))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})

		It("best-effort/none pass through even for cross-zone requests", func(ctx context.Context) {
			node := passthroughNode(ctx, testCtx)
			gpus, ok := node.SpanTwoZonesGPUs()
			if !ok {
				gpus, ok = node.OneZoneGPUs()
			}
			if !ok {
				Skip("no best-effort/none node exposes a GPU-bearing zone")
			}

			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), gpus)))
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})

		It("best-effort - an active single-zone memory group blocks a two-zone request despite sufficient total capacity", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicyBestEffort, MinZones: 2})
			if len(node.Zones) != 2 {
				Skip("need a best-effort node with exactly two NUMA zones")
			}
			zoneMemory := node.MaxZoneMemory()
			holderMemory := *resource.NewQuantity(zoneMemory.Value()/2, resource.BinarySI)
			probeMemory := zoneMemory.DeepCopy()
			probeMemory.Add(*resource.NewQuantity(zoneMemory.Value()/4, resource.BinarySI))
			combinedMemory := probeMemory.DeepCopy()
			combinedMemory.Add(holderMemory)
			totalCPU := node.TotalCPU()
			if holderMemory.IsZero() || probeMemory.Cmp(zoneMemory) <= 0 ||
				combinedMemory.Cmp(node.TotalMemory()) > 0 || totalCPU.Cmp(resource.MustParse("200m")) < 0 {
				Skip("need capacity for a half-zone holder and a 1.25-zone probe within the node's total resources")
			}

			holder := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedPod(childQueue(testCtx), v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: holderMemory,
			})))
			expectGuaranteed(ctx, testCtx, holder)
			wait.ForPodReady(ctx, testCtx.ControllerClient, holder)
			Eventually(func(g Gomega) {
				fresh, err := testCtx.KubeClientset.CoreV1().Pods(holder.Namespace).Get(ctx, holder.Name, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				var groups []schedulingv1alpha2.NUMAMemoryGroupPlacement
				g.Expect(json.Unmarshal([]byte(fresh.Annotations[constants.NumaMemoryGroupsObserved]), &groups)).To(Succeed())
				g.Expect(groups).To(HaveLen(1))
				g.Expect(groups[0].MemoryNodes).To(HaveLen(1))
				amount, exists := groups[0].Amount[v1.ResourceMemory]
				g.Expect(exists).To(BeTrue())
				g.Expect(amount.Cmp(holderMemory)).To(BeZero())
			}, placementTimeout, 2*time.Second).Should(Succeed())

			probe := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedPod(childQueue(testCtx), v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: probeMemory,
			})))
			expectGuaranteed(ctx, testCtx, probe)
			expectMemoryConflict := func(g Gomega) {
				fresh, err := testCtx.KubeClientset.CoreV1().Pods(probe.Namespace).Get(ctx, probe.Name, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				g.Expect(fresh.Spec.NodeName).To(BeEmpty(), "the scheduler must reject the pod before kubelet admission")
				g.Expect(fresh.Status.Phase).To(Equal(v1.PodPending))
				g.Expect(fresh.Status.Conditions).To(ContainElement(SatisfyAll(
					HaveField("Type", Equal(v1.PodScheduled)),
					HaveField("Status", Equal(v1.ConditionFalse)),
					HaveField("Reason", Equal(v1.PodReasonUnschedulable)),
					HaveField("Message", ContainSubstring("NUMA memory placement conflict")),
				)))
			}
			Eventually(expectMemoryConflict, placementTimeout, time.Second).Should(Succeed())
		})

		It("node without NRT passes through", func(ctx context.Context) {
			// A CPU-only Guaranteed pod is enough: without an NRT object the plugin never handles the pod,
			// so it schedules purely on ordinary capacity.
			pod := createPod(ctx, testCtx, numautil.GuaranteedPod(childQueue(testCtx), v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("250m"),
				v1.ResourceMemory: resource.MustParse("128Mi"),
			}))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
}

// firstMatch discovers a node matching req (skipping the spec if none) and returns the first match.
func firstMatch(ctx context.Context, testCtx *testcontext.TestContext, req numautil.Requirement) numautil.Node {
	return numautil.RequireNodes(ctx, testCtx.ControllerClient, req)[0]
}

// passthroughNode returns a best-effort or none node, skipping when neither is present.
func passthroughNode(ctx context.Context, testCtx *testcontext.TestContext) numautil.Node {
	nodes, err := numautil.List(ctx, testCtx.ControllerClient)
	if err != nil {
		Skip("NodeResourceTopology not available; skipping NUMA test")
	}
	for _, node := range nodes {
		if node.Policy == numautil.PolicyBestEffort || node.Policy == numautil.PolicyNone {
			return node
		}
	}
	Skip("no best-effort/none NUMA node found")
	return numautil.Node{}
}
