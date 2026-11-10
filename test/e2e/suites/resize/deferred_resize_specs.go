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
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	resizerInitialMilliCPU = 500
	nodeSlackMilliCPU      = 200
	minFillerMilliCPU      = 400
	deferredResizeQuietFor = 30 * time.Second
)

// The resize fits only after one of the two fillers is gone.
type deferredResizeSetup struct {
	resizer   *v1.Pod
	fillers   []*v1.Pod
	targetCPU string
}

func DescribeDeferredResizeSpecs() bool {
	return Describe("Deferred in-place pod resize", Ordered, func() {
		var (
			testCtx           *testcontext.TestContext
			testQueue         *v2.Queue
			overShareQueue    *v2.Queue
			reclaimerQueue    *v2.Queue
			lowPriorityClass  string
			highPriorityClass string
		)

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			skipIfNoResizeSupport(testCtx)
			capacity.SkipIfInsufficientClusterResources(testCtx.KubeClientset,
				&capacity.ResourceList{Cpu: resource.MustParse("2"), PodCount: 3})

			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			testQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			// No deserved quota, so all of it is reclaimable.
			overShareQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			overShareQueue.Spec.Resources.CPU.Quota = 0
			overShareQueue.Spec.Resources.CPU.OverQuotaWeight = 0
			overShareQueue.Spec.Resources.Memory.Quota = 0
			overShareQueue.Spec.Resources.Memory.OverQuotaWeight = 0
			// Above any kind cluster's size, so the resize stays within fair share.
			reclaimerQueue = queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			reclaimerQueue.Spec.Resources.CPU.Quota = 1000000
			reclaimerQueue.Spec.Resources.Memory.Quota = 1000000
			testCtx.InitQueues([]*v2.Queue{testQueue, overShareQueue, reclaimerQueue, parentQueue})

			lowPriorityValue := utils.RandomIntBetween(0, constant.NonPreemptiblePriorityThreshold-2)
			lowPriorityClass = utils.GenerateRandomK8sName(10)
			_, err := testCtx.KubeClientset.SchedulingV1().PriorityClasses().
				Create(ctx, rd.CreatePriorityClass(lowPriorityClass, lowPriorityValue), metav1.CreateOptions{})
			Expect(err).To(Succeed())
			highPriorityClass = utils.GenerateRandomK8sName(10)
			_, err = testCtx.KubeClientset.SchedulingV1().PriorityClasses().
				Create(ctx, rd.CreatePriorityClass(highPriorityClass, lowPriorityValue+1), metav1.CreateOptions{})
			Expect(err).To(Succeed())
		})

		AfterEach(func(ctx context.Context) {
			testCtx.TestContextCleanup(ctx)
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, nil)).
				To(Succeed(), "failed to restore deferred-resize-preemption default")
			Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		It("keeps a deferred resize pending when deferred resize preemption is disabled", func(ctx context.Context) {
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, ptr.To(false))).To(Succeed())
			setup := createDeferredResize(ctx, testCtx, testQueue, testQueue, highPriorityClass, lowPriorityClass)

			Consistently(func(g Gomega) {
				g.Expect(isPodResizeDeferred(ctx, testCtx, setup.resizer)).To(BeTrue())
				for _, filler := range setup.fillers {
					g.Expect(isPodTerminating(ctx, testCtx, filler)).To(BeFalse(),
						"filler %s must not be evicted", filler.Name)
				}
			}).WithContext(ctx).WithTimeout(deferredResizeQuietFor).WithPolling(resizePoll).Should(Succeed())
		})

		It("evicts a lower-priority pod on the node so the deferred resize is enacted", func(ctx context.Context) {
			// With the flag on, room may be freed before a poll sees Deferred.
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, ptr.To(false))).To(Succeed())
			setup := createDeferredResize(ctx, testCtx, testQueue, testQueue, highPriorityClass, lowPriorityClass)
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, ptr.To(true))).To(Succeed())
			expectResizeEnactedAfterOneEviction(ctx, testCtx, setup)
		})

		It("reclaims from a queue over its fair share so the deferred resize is enacted", func(ctx context.Context) {
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, ptr.To(false))).To(Succeed())
			setup := createDeferredResize(ctx, testCtx, reclaimerQueue, overShareQueue, lowPriorityClass, lowPriorityClass)
			Expect(feature_flags.SetDeferredResizePreemption(ctx, testCtx, ptr.To(true))).To(Succeed())
			expectResizeEnactedAfterOneEviction(ctx, testCtx, setup)
		})
	})
}

func expectResizeEnactedAfterOneEviction(ctx context.Context, testCtx *testcontext.TestContext, setup *deferredResizeSetup) {
	countEvicted := func() int {
		evicted := 0
		for _, filler := range setup.fillers {
			if isPodTerminating(ctx, testCtx, filler) {
				evicted++
			}
		}
		return evicted
	}
	Eventually(countEvicted).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).
		Should(BeNumerically(">=", 1))

	expectEnactedCPU(ctx, testCtx, setup.resizer, setup.targetCPU)
	Eventually(func() bool {
		return hasResizePendingCondition(ctx, testCtx, setup.resizer)
	}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).Should(BeFalse())

	Consistently(countEvicted).WithContext(ctx).WithTimeout(deferredResizeQuietFor).WithPolling(resizePoll).
		Should(Equal(1), "only one filler is needed for the resize")
}

func createDeferredResize(
	ctx context.Context, testCtx *testcontext.TestContext, resizerQueue, fillerQueue *v2.Queue,
	resizerPriorityClass, fillerPriorityClass string,
) *deferredResizeSetup {
	resizer := rd.CreatePodObject(resizerQueue, cpuRequest(resizerInitialMilliCPU))
	resizer.Spec.PriorityClassName = resizerPriorityClass
	resizer, err := rd.CreatePod(ctx, testCtx.KubeClientset, resizer)
	Expect(err).NotTo(HaveOccurred(), "failed to create resizer pod")
	wait.ForPodReady(ctx, testCtx.ControllerClient, resizer)
	Expect(testCtx.ControllerClient.Get(ctx,
		types.NamespacedName{Namespace: resizer.Namespace, Name: resizer.Name}, resizer)).To(Succeed())
	nodeName := resizer.Spec.NodeName

	idle, err := capacity.GetNodesIdleResources(testCtx.KubeClientset)
	Expect(err).NotTo(HaveOccurred())
	fillerMilliCPU := (idle[nodeName].Cpu.MilliValue() - nodeSlackMilliCPU) / 2
	if fillerMilliCPU < minFillerMilliCPU {
		Skip(fmt.Sprintf("node %s has too little idle cpu to fill it", nodeName))
	}

	setup := &deferredResizeSetup{
		resizer:   resizer,
		targetCPU: fmt.Sprintf("%dm", resizerInitialMilliCPU+nodeSlackMilliCPU+fillerMilliCPU/2),
	}
	for range 2 {
		filler := rd.CreatePodObject(fillerQueue, cpuRequest(fillerMilliCPU))
		filler.Spec.PriorityClassName = fillerPriorityClass
		filler.Spec.NodeSelector = map[string]string{v1.LabelHostname: nodeName}
		filler, err = rd.CreatePod(ctx, testCtx.KubeClientset, filler)
		Expect(err).NotTo(HaveOccurred(), "failed to create filler pod")
		wait.ForPodReady(ctx, testCtx.ControllerClient, filler)
		setup.fillers = append(setup.fillers, filler)
	}

	By(fmt.Sprintf("resizing %s to %s on full node %s", resizer.Name, setup.targetCPU, nodeName))
	Expect(resizeCPU(ctx, testCtx, resizer, setup.targetCPU)).To(Succeed())
	Eventually(func() bool {
		return isPodResizeDeferred(ctx, testCtx, resizer)
	}).WithContext(ctx).WithTimeout(resizeTimeout).WithPolling(resizePoll).Should(BeTrue(),
		"kubelet should mark the resize Deferred")
	return setup
}

func cpuRequest(milliCPU int64) v1.ResourceRequirements {
	return v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
			v1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}
}

func isPodResizeDeferred(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) bool {
	current := &v1.Pod{}
	if err := testCtx.ControllerClient.Get(ctx,
		types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current); err != nil {
		return false
	}
	for _, c := range current.Status.Conditions {
		if c.Type == v1.PodResizePending {
			return c.Status == v1.ConditionTrue && c.Reason == v1.PodReasonDeferred
		}
	}
	return false
}

func hasResizePendingCondition(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) bool {
	current := &v1.Pod{}
	Expect(testCtx.ControllerClient.Get(ctx,
		types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current)).To(Succeed())
	for _, c := range current.Status.Conditions {
		if c.Type == v1.PodResizePending && c.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func isPodTerminating(ctx context.Context, testCtx *testcontext.TestContext, pod *v1.Pod) bool {
	current := &v1.Pod{}
	err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, current)
	if errors.IsNotFound(err) {
		return true
	}
	Expect(err).NotTo(HaveOccurred())
	return current.DeletionTimestamp != nil
}
