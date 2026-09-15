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
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAQoSSpecs asserts that only Guaranteed pods are subject to NUMA alignment: a Guaranteed pod
// with a cross-zone request is filtered, while Burstable/BestEffort equivalents pass through.
func DescribeNUMAQoSSpecs() bool {
	return Describe("NUMA QoS gating", Ordered, Serial, Label("numa", "nightly"), func() {
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

		It("Guaranteed cross-zone request is filtered", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode, MinZones: 2})
			gpus, ok := node.SpanTwoZonesGPUs()
			if !ok {
				Skip("no single-numa-node node can express a two-zone-spanning request")
			}
			pod := createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(childQueue(testCtx), gpus)))
			expectGuaranteed(ctx, testCtx, pod)
			wait.ForPodUnschedulable(ctx, testCtx.ControllerClient, pod)
		})

		It("Burstable cross-zone request passes through", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode, MinZones: 2})
			gpus, ok := node.SpanTwoZonesGPUs()
			if !ok {
				Skip("no single-numa-node node can express a two-zone-spanning request")
			}
			// Burstable: GPU limit without matching CPU/memory requests==limits.
			pod := node.Pin(rd.CreatePodObject(childQueue(testCtx), v1.ResourceRequirements{
				Limits:   v1.ResourceList{constants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI)},
				Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
			}))
			created := createPod(ctx, testCtx, pod)
			expectNotGuaranteed(ctx, testCtx, created)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, created)
		})

		It("BestEffort pod passes through", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode})
			pod := createPod(ctx, testCtx, node.Pin(rd.CreatePodObject(childQueue(testCtx), v1.ResourceRequirements{})))
			expectNotGuaranteed(ctx, testCtx, pod)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
}
