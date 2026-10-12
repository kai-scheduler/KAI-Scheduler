// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAModesSpecs covers the per-node Topology Manager modes: single-numa-node and restricted
// filter Guaranteed pods the kubelet would reject, while best-effort/none/no-NRT pass through. The suite
// mutates the shard plugin config, so it runs Serial.
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
