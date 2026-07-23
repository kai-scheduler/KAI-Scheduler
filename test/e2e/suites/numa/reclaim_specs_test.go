// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

// DescribeNUMAReclaimSpecs asserts NUMA constraints are honored when a queue reclaims resources: the
// reclaimer's Guaranteed pod only schedules on zones the reclaim actually frees. Restricted is covered
// for single-zone (width 1) and multi-zone (width 2) reclaimers, including fragmented victims and gangs.
func DescribeNUMAReclaimSpecs() bool {
	return Describe("NUMA reclaim", Ordered, Serial, Label("numa", "nightly"), func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		It("single-numa-node - reclaim frees one zone width 1", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicySingleNUMANode)
			zone := node.MaxZoneGPUs()
			runReclaim(ctx, testCtx, node, zone, zone)
		})

		It("restricted - reclaim frees one zone width 1", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicyRestricted)
			zone := node.MaxZoneGPUs()
			runReclaim(ctx, testCtx, node, zone, zone)
		})

		It("restricted - two-zone reclaim from full-zone victims width 2", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicyRestricted)
			zone := node.MaxZoneGPUs()
			// One full-zone victim per zone; a (zone+1)-GPU reclaimer must evict a victim from each zone.
			runReclaim(ctx, testCtx, node, zone, zone+1)
		})

		It("restricted - two-zone reclaim from fragmented victims width 2", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicyRestricted)
			zone := node.MaxZoneGPUs()
			// Single-GPU victims spread across both zones; the reclaimer partially drains each zone.
			runReclaim(ctx, testCtx, node, 1, zone+1)
		})

		It("restricted - full-node reclaim across both zones width 2", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicyRestricted)
			runReclaim(ctx, testCtx, node, node.MaxZoneGPUs(), node.TotalGPUs())
		})

		It("restricted - gang reclaim spanning both zones", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			node := reclaimNode(ctx, testCtx, numautil.PolicyRestricted)
			runGangReclaim(ctx, testCtx, node)
		})
	})
}

func reclaimNode(ctx context.Context, testCtx *testcontext.TestContext, policy string) numautil.Node {
	return numautil.RequireNodes(ctx, testCtx.ControllerClient,
		numautil.Requirement{Policy: policy, MinZones: 2, ZoneGPUs: 1})[0]
}

// runReclaim fills a node's GPUs with victimGPUs-sized pods from an over-quota reclaimee queue, then
// submits a single reclaimerGPUs-sized pod in an under-quota queue and asserts the scheduler reclaims to
// place it. The reclaimer spans one zone when it fits a zone, two zones otherwise. Only the reclaimer's
// placement is asserted (zone count + total GPU); victim eviction order is not deterministic.
func runReclaim(ctx context.Context, testCtx *testcontext.TestContext, node numautil.Node, victimGPUs, reclaimerGPUs int64) {
	maxZone := node.MaxZoneGPUs()
	total := node.TotalGPUs()
	if victimGPUs < 1 || victimGPUs > maxZone {
		Skip("victim size must fit within a single zone")
	}
	if reclaimerGPUs < 1 || reclaimerGPUs > total {
		Skip("reclaimer must fit within the node's GPU capacity")
	}
	if total%victimGPUs != 0 {
		Skip("victim size does not evenly fill the node")
	}
	fillerCount := int(total / victimGPUs)
	if fillerCount < 2 {
		Skip("need at least two victims to occupy both zones")
	}

	wantZones := 1
	if reclaimerGPUs > maxZone {
		wantZones = 2
	}

	// Entitle the reclaimer to the whole node, not just reclaimerGPUs: with full-zone atomic victims a
	// width-2 reclaimer must evict a whole victim per zone (up to `total` GPUs) to place its request, so
	// a reclaimerGPUs-sized quota would stop reclaim early and leave a surviving victim that pushes the
	// parent over its limit. The pod still only requests reclaimerGPUs; reclaimee stays fully reclaimable.
	parent, reclaimee, reclaimer := gpuQueuesTriple(float64(total), 0, float64(total))
	testCtx.InitQueues([]*v2.Queue{reclaimee, reclaimer, parent})

	fillNode(ctx, testCtx, node, reclaimee, victimGPUs, fillerCount)

	reclaimerPod := createPod(ctx, testCtx, reclaimerPodFor(node, reclaimer, reclaimerGPUs, wantZones))
	wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimerPod)
	expectGPUPlacement(ctx, testCtx, reclaimerPod, wantZones, reclaimerGPUs)
}

// runGangReclaim fills every zone from a reclaimee queue, then submits a gang of one full-zone pod per
// zone. The gang schedules only if reclaim frees every zone; the pods must land in distinct zones.
func runGangReclaim(ctx context.Context, testCtx *testcontext.TestContext, node numautil.Node) {
	maxZone := node.MaxZoneGPUs()
	total := node.TotalGPUs()
	podCount := int(total / maxZone)
	if podCount < 2 || maxZone < 1 {
		Skip("need at least two full zones for a cross-zone gang")
	}

	parent, reclaimee, reclaimer := gpuQueuesTriple(float64(total), 0, float64(total))
	testCtx.InitQueues([]*v2.Queue{reclaimee, reclaimer, parent})

	fillNode(ctx, testCtx, node, reclaimee, maxZone, podCount)

	// One full-zone pod per zone, submitted as a single Job pinned to the node. The podgrouper gangs the
	// Job (minMember = parallelism), so it schedules all-or-nothing only once reclaim frees every zone.
	job := rd.CreateBatchJobObject(reclaimer, numautil.GuaranteedGPURequirements(maxZone))
	job.Spec.Parallelism = ptr.To(int32(podCount))
	job.Spec.Completions = ptr.To(int32(podCount))
	job.Spec.Template.Spec.NodeSelector = node.HostSelector()
	job, err := testCtx.KubeClientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	var gangPods []v1.Pod
	Eventually(func(g Gomega) {
		gangPods = rd.GetJobPods(ctx, testCtx.KubeClientset, job)
		g.Expect(gangPods).To(HaveLen(podCount))
	}, time.Minute, time.Second).Should(Succeed())
	for i := range gangPods {
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, &gangPods[i])
	}

	// Each gang pod fills one zone; together they must occupy distinct zones spanning the node.
	zones := map[string]bool{}
	for i := range gangPods {
		observed := awaitObservedZones(ctx, testCtx, &gangPods[i])
		Expect(observed).To(HaveLen(1))
		Expect(observedGPUs(observed)).To(Equal(maxZone))
		zones[observed[0].Zone] = true
	}
	Expect(zones).To(HaveLen(podCount), "gang pods should occupy distinct zones spanning the node")
}

func fillNode(ctx context.Context, testCtx *testcontext.TestContext, node numautil.Node, queue *v2.Queue, gpusPerPod int64, count int) {
	fillers := make([]*v1.Pod, 0, count)
	for range count {
		fillers = append(fillers, createPod(ctx, testCtx, node.Pin(numautil.GuaranteedGPUPod(queue, gpusPerPod))))
	}
	for _, filler := range fillers {
		wait.ForPodScheduled(ctx, testCtx.ControllerClient, filler)
	}
}

// reclaimerPodFor builds the reclaimer. A multi-zone GPU request must also carry a width-2 CPU request:
// otherwise its minimal CPU width (1) conflicts with the GPU width under restricted and the pod is
// rejected before any reclaim happens.
func reclaimerPodFor(node numautil.Node, queue *v2.Queue, gpus int64, wantZones int) *v1.Pod {
	if wantZones < 2 {
		return node.Pin(numautil.GuaranteedGPUPod(queue, gpus))
	}
	cpu, cpuOK := node.TwoZoneCPU()
	memory, memOK := node.TwoZoneMemory()
	if !cpuOK || !memOK {
		Skip("cannot build a width-2 CPU/memory request on the discovered node")
	}
	return node.Pin(numautil.GuaranteedGPUCPUPod(queue, gpus, cpu, memory))
}
