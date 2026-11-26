// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAQoSSpecs checks that device alignment applies across QoS classes
// while CPU and memory alignment does not apply to Burstable pods.
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

		It("Burstable cross-zone GPU request is filtered", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode, MinZones: 2})
			gpus, ok := node.SpanTwoZonesGPUs()
			if !ok {
				Skip("no single-numa-node node can express a two-zone-spanning request")
			}
			pod := node.Pin(rd.CreatePodObject(childQueue(testCtx), v1.ResourceRequirements{
				Limits:   v1.ResourceList{constants.NvidiaGpuResource: *resource.NewQuantity(gpus, resource.DecimalSI)},
				Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
			}))
			created := createPod(ctx, testCtx, pod)
			expectQoS(ctx, testCtx, created, v1.PodQOSBurstable)
			Eventually(func(g Gomega) {
				fresh, err := testCtx.KubeClientset.CoreV1().Pods(created.Namespace).Get(ctx, created.Name, metav1.GetOptions{})
				g.Expect(err).To(Succeed())
				g.Expect(fresh.Spec.NodeName).To(BeEmpty())
				g.Expect(fresh.Status.Conditions).To(ContainElement(SatisfyAll(
					HaveField("Type", Equal(v1.PodScheduled)),
					HaveField("Status", Equal(v1.ConditionFalse)),
					HaveField("Reason", Equal(v1.PodReasonUnschedulable)),
					HaveField("Message", ContainSubstring("no preferred NUMA affinity for single-numa-node")),
				)))
			}, placementTimeout, time.Second).Should(Succeed())
		})

		It("Burstable pod can use CPU and memory beyond its GPU zone", func(ctx context.Context) {
			nodes := numautil.RequireNodes(ctx, testCtx.ControllerClient, numautil.Requirement{
				Policy: numautil.PolicySingleNUMANode, MinZones: 2, ZoneGPUs: 1,
			})
			for _, node := range nodes {
				cpu, cpuOK := node.TwoZoneCPU()
				memory, memoryOK := node.TwoZoneMemory()
				if !cpuOK || !memoryOK {
					continue
				}
				worker, err := testCtx.KubeClientset.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
				Expect(err).To(Succeed())
				if cpu.Cmp(worker.Status.Allocatable[v1.ResourceCPU]) > 0 ||
					memory.Cmp(worker.Status.Allocatable[v1.ResourceMemory]) > 0 {
					continue
				}

				pod := createPod(ctx, testCtx, node.Pin(rd.CreatePodObject(childQueue(testCtx), v1.ResourceRequirements{
					Requests: v1.ResourceList{v1.ResourceCPU: cpu, v1.ResourceMemory: memory},
					Limits:   v1.ResourceList{constants.NvidiaGpuResource: *resource.NewQuantity(1, resource.DecimalSI)},
				})))
				expectQoS(ctx, testCtx, pod, v1.PodQOSBurstable)
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
				return
			}
			Skip("need a single-numa-node worker with node capacity for CPU and memory requests larger than one zone")
		})

		It("BestEffort pod passes through", func(ctx context.Context) {
			node := firstMatch(ctx, testCtx, numautil.Requirement{Policy: numautil.PolicySingleNUMANode})
			pod := createPod(ctx, testCtx, node.Pin(rd.CreatePodObject(childQueue(testCtx), v1.ResourceRequirements{})))
			expectQoS(ctx, testCtx, pod, v1.PodQOSBestEffort)
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)
		})
	})
}
