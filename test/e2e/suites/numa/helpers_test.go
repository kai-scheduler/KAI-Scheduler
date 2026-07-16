// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	numautil "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/numa"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	npeDaemonSetName = "numa-placement-exporter"
	reconcileTimeout = 2 * time.Minute
	reconcilePoll    = 3 * time.Second
	placementTimeout = 2 * time.Minute
)

// gpuQueues builds a parent/child queue pair with the given GPU quota and an unbounded limit, so NUMA
// tests can freely allocate GPUs without hitting queue limits.
func gpuQueues(gpuQuota float64) (parent, child *v2.Queue) {
	parentName := utils.GenerateRandomK8sName(10)
	parent = queue.CreateQueueObjectWithGpuResource(parentName,
		v2.QueueResource{Quota: gpuQuota, Limit: -1, OverQuotaWeight: 1}, "")
	child = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
		v2.QueueResource{Quota: gpuQuota, Limit: -1, OverQuotaWeight: 1}, parentName)
	return parent, child
}

// childQueue returns the leaf queue tests submit pods into (gpuQueues installs it as Queues[0]).
func childQueue(testCtx *testcontext.TestContext) *v2.Queue {
	return testCtx.Queues[0]
}

// gpuQueuesTriple builds a parent with two leaf queues (reclaimee, reclaimer) for reclaim scenarios.
func gpuQueuesTriple(parentQuota, reclaimeeQuota, reclaimerQuota float64) (parent, reclaimee, reclaimer *v2.Queue) {
	parentName := utils.GenerateRandomK8sName(10)
	parent = queue.CreateQueueObjectWithGpuResource(parentName,
		v2.QueueResource{Quota: parentQuota, Limit: parentQuota, OverQuotaWeight: 1}, "")
	reclaimee = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
		v2.QueueResource{Quota: reclaimeeQuota, Limit: -1, OverQuotaWeight: 1}, parentName)
	reclaimer = queue.CreateQueueObjectWithGpuResource(utils.GenerateRandomK8sName(10),
		v2.QueueResource{Quota: reclaimerQuota, Limit: -1, OverQuotaWeight: 1}, parentName)
	return parent, reclaimee, reclaimer
}

func createPod(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) *v1.Pod {
	created, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).To(Succeed())
	return created
}

// expectGuaranteed asserts the (already created) pod was admitted as Guaranteed QoS, the only class the
// numa plugin acts on.
func expectGuaranteed(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) {
	Eventually(func(g Gomega) {
		fetched, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		g.Expect(err).To(Succeed())
		g.Expect(fetched.Status.QOSClass).To(Equal(v1.PodQOSGuaranteed))
	}, 30*time.Second, time.Second).Should(Succeed())
}

// expectNotGuaranteed asserts the pod was admitted with a non-Guaranteed QoS class, so the numa plugin
// leaves it alone.
func expectNotGuaranteed(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) {
	Eventually(func(g Gomega) {
		fetched, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		g.Expect(err).To(Succeed())
		g.Expect(fetched.Status.QOSClass).ToNot(Equal(v1.PodQOSGuaranteed))
	}, 30*time.Second, time.Second).Should(Succeed())
}

// expectGPUPlacement waits for the NPE to publish the pod's observed NUMA placement, then asserts it
// spans exactly wantZones zones and accounts for wantGPUs GPUs in total. Only zone count and total GPU
// are checked (not per-zone splits or specific zone ids): when several pods are scheduled together the
// exact split depends on bind order, but a request's zone width and total are deterministic. Requires
// the real exporter to be running (enabling the numa plugin auto-deploys it).
func expectGPUPlacement(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod, wantZones int, wantGPUs int64) {
	observed := awaitObservedZones(ctx, testCtx, pod)
	Expect(observed).To(HaveLen(wantZones), "observed placement should span %d NUMA zone(s)", wantZones)
	Expect(observedGPUs(observed)).To(Equal(wantGPUs), "observed placement should account for %d GPU(s)", wantGPUs)
}

// awaitObservedZones waits for the NPE to publish the pod's observed NUMA placement and returns it.
func awaitObservedZones(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) []schedulingv1alpha2.NUMAZonePlacement {
	var observed []schedulingv1alpha2.NUMAZonePlacement
	Eventually(func(g Gomega) {
		fresh, err := testCtx.KubeClientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		g.Expect(err).To(Succeed())
		observed = numautil.ObservedZones(fresh)
		g.Expect(observed).ToNot(BeEmpty(), "waiting for the NUMA placement exporter to publish observed placement")
	}, placementTimeout, 2*time.Second).Should(Succeed())
	return observed
}

func observedGPUs(zones []schedulingv1alpha2.NUMAZonePlacement) int64 {
	var total int64
	for _, zone := range zones {
		if qty, ok := zone.Amount[constants.NvidiaGpuResource]; ok {
			total += qty.Value()
		}
	}
	return total
}

// kaiNamespace returns the namespace KAI components (including the NPE DaemonSet) are deployed into.
func kaiNamespace(ctx context.Context, testCtx *testcontext.TestContext) string {
	cfg := &kaiv1.Config{}
	Expect(testCtx.ControllerClient.Get(
		ctx, runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, cfg)).To(Succeed())
	return cfg.Spec.Namespace
}

// expectNPEDaemonSet waits until the NUMA placement exporter DaemonSet is present or pruned.
func expectNPEDaemonSet(ctx context.Context, testCtx *testcontext.TestContext, shouldExist bool) {
	namespace := kaiNamespace(ctx, testCtx)
	Eventually(func(g Gomega) {
		ds := &appsv1.DaemonSet{}
		err := testCtx.ControllerClient.Get(
			ctx, runtimeClient.ObjectKey{Namespace: namespace, Name: npeDaemonSetName}, ds)
		if shouldExist {
			g.Expect(err).To(Succeed())
			return
		}
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected DaemonSet to be absent, got err=%v", err)
	}, reconcileTimeout, reconcilePoll).Should(Succeed())
}
