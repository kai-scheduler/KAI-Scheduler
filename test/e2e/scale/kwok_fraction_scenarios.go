// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/api/kai/v1/common"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	fractionDefaultGPUMemory  = 81920
	fractionContainerName     = "ubuntu-container"
	fractionCreateConcurrency = 100
	fractionCreateBatchSize   = 1000
	fractionCreateRetries     = 10
	fractionConfigTimeout     = 5 * time.Minute

	fractionCreateBackoffStart = time.Second
	fractionCreateBackoffMax   = 30 * time.Second
	fractionBatchPauseStart    = 2 * time.Second
	fractionBatchPauseMax      = time.Minute

	saturationAllocationScenarioName = "Mixed fractions saturation - allocation"
	saturationSchedulingScenarioName = "Mixed fractions saturation - scheduling"
)

// fractionTier is a queue whose pods each request 1/fractionsPerGPU of a GPU's memory.
type fractionTier struct {
	queue           *v2.Queue
	fractionsPerGPU int
}

// allocatedFractionPod is a pod the scheduler allocated, with the creation time of its BindRequest.
type allocatedFractionPod struct {
	pod             v1.Pod
	fractionsPerGPU int
	allocatedAt     time.Time
}

var fractionPodRequirement = v1.ResourceRequirements{
	Requests: v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("100m"),
		v1.ResourceMemory: resource.MustParse("128Mi"),
	},
}

func fractionMemoryMiB(gpuMemoryMiB int64, fractionsPerGPU int) int {
	return int(gpuMemoryMiB) / fractionsPerGPU
}

func fractionMemoryAnnotationKey() string {
	return constants.NvFractionsAnnotationPrefix + fractionContainerName + constants.NvFractionsMemoryRequestSuffix
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[max(index, 0)]
}

// validateFractionPacking checks that every pod was bound to a GPU group and that no group exceeds one GPU's memory.
// Returns the number of distinct GPU groups used.
func validateFractionPacking(pods []v1.Pod, gpuMemoryMiB int64) (int, error) {
	type groupKey struct{ node, group string }
	usedMiB := map[groupKey]int64{}
	var errs error
	for i := range pods {
		pod := &pods[i]
		group := pod.Labels[constants.GPUGroup]
		if pod.Spec.NodeName == "" || group == "" {
			errs = errors.Join(errs, fmt.Errorf("pod %s has no node or GPU group", pod.Name))
			continue
		}
		request, err := resource.ParseQuantity(pod.Annotations[fractionMemoryAnnotationKey()])
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("pod %s has no valid memory request: %w", pod.Name, err))
			continue
		}
		usedMiB[groupKey{pod.Spec.NodeName, group}] += request.Value() / (1 << 20)
	}
	for key, used := range usedMiB {
		if used > gpuMemoryMiB {
			errs = errors.Join(errs, fmt.Errorf("GPU group %s on node %s over-committed: %dMi > %dMi",
				key.group, key.node, used, gpuMemoryMiB))
		}
	}
	return len(usedMiB), errs
}

func setGpuSharingMode(ctx context.Context, testCtx *testcontext.TestContext, mode *common.GpuSharingMode) {
	Expect(schedulerconfig.PatchKAIConfig(ctx, testCtx, func(config *kaiv1.Config) {
		if config.Spec.Global == nil {
			config.Spec.Global = &kaiv1.GlobalConfig{}
		}
		config.Spec.Global.GpuSharingMode = mode
	})).To(Succeed())
	waitForConfigDeployed(ctx, testCtx)
}

// DependenciesFulfilled is excluded: NvFractions mode reports it False without a gpu-fractioning operator,
// which the scale environment intentionally lacks.
func waitForConfigDeployed(ctx context.Context, testCtx *testcontext.TestContext) {
	Eventually(func(g Gomega) {
		config := &kaiv1.Config{}
		g.Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{
			Name: constants.DefaultKAIConfigSingeltonInstanceName}, config)).To(Succeed())
		for _, conditionType := range []kaiv1.ConditionType{kaiv1.ConditionTypeDeployed, kaiv1.ConditionTypeAvailable} {
			condition := meta.FindStatusCondition(config.Status.Conditions, string(conditionType))
			g.Expect(condition).NotTo(BeNil(), "condition %s missing", conditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "condition %s not true", conditionType)
			g.Expect(condition.ObservedGeneration).To(Equal(config.Generation), "condition %s is stale", conditionType)
		}
	}, fractionConfigTimeout, podsPollIntervalSeconds*time.Second).Should(Succeed())
}

func currentGpuSharingMode(ctx context.Context, testCtx *testcontext.TestContext) *common.GpuSharingMode {
	config := &kaiv1.Config{}
	Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{
		Name: constants.DefaultKAIConfigSingeltonInstanceName}, config)).To(Succeed())
	if config.Spec.Global == nil {
		return nil
	}
	return config.Spec.Global.GpuSharingMode
}

// stampNvFractionsNodes gives KWOK nodes what the scheduler reads in NvFractions mode, since no gpu-fractioning operator
// manages them: the GPU memory label and the Ready condition. Returns the per-GPU memory in MiB.
func stampNvFractionsNodes(ctx context.Context, testCtx *testcontext.TestContext) int64 {
	nodes := &v1.NodeList{}
	Expect(testCtx.ControllerClient.List(ctx, nodes, runtimeClient.MatchingLabels{"type": "kwok"})).To(Succeed())
	Expect(nodes.Items).NotTo(BeEmpty(), "no KWOK nodes found")

	gpuMemoryMiB := int64(fractionDefaultGPUMemory)
	if existing, err := strconv.ParseInt(nodes.Items[0].Labels[constants.NvidiaGpuMemory], 10, 64); err == nil {
		gpuMemoryMiB = existing
	}

	labelPatch := []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:"%d"}}}`, constants.NvidiaGpuMemory, gpuMemoryMiB))
	conditionPatch := []byte(fmt.Sprintf(
		`{"status":{"conditions":[{"type":%q,"status":"True","reason":"ScaleTest"}]}}`,
		constants.NvFractionNodeReadyConditionType))

	semaphore := make(chan struct{}, fractionCreateConcurrency)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var patchErr error
	for i := range nodes.Items {
		node := &nodes.Items[i]
		wg.Add(1)
		semaphore <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			err := testCtx.ControllerClient.Patch(ctx, node.DeepCopy(),
				runtimeClient.RawPatch(types.StrategicMergePatchType, labelPatch))
			if err == nil {
				err = testCtx.ControllerClient.Status().Patch(ctx, node.DeepCopy(),
					runtimeClient.RawPatch(types.StrategicMergePatchType, conditionPatch))
			}
			if err != nil {
				lock.Lock()
				patchErr = errors.Join(patchErr, err)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	Expect(patchErr).NotTo(HaveOccurred(), "failed to stamp NvFractions node state")
	return gpuMemoryMiB
}

func newFractionPod(testQueue *v2.Queue, memoryMiB int) *v1.Pod {
	pod := rd.CreatePodObject(testQueue, fractionPodRequirement)
	pod.Annotations[fractionMemoryAnnotationKey()] = fmt.Sprintf("%dMi", memoryMiB)
	addKWOKTaintsAndAffinity(&pod.Spec)
	return pod
}

// createFractionPods creates the pods in batches. Tens of thousands of pod creates, plus the PodGroups the pod-grouper
// writes for them, can exceed the API server's etcd write budget (HTTP 429 "etcdserver: throttle"). That is a limit of
// the hosting cluster, not something this test measures, so throttled creates are retried with backoff and a batch that
// saw throttling delays the next one, doubling the pause until a batch goes through cleanly.
func createFractionPods(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, podCount, memoryMiB int,
) {
	var throttled atomic.Int64
	pause := time.Duration(0)
	for created := 0; created < podCount; created += fractionCreateBatchSize {
		batch := min(fractionCreateBatchSize, podCount-created)
		throttledBefore := throttled.Load()
		Expect(createFractionPodBatch(ctx, testCtx, testQueue, batch, memoryMiB, &throttled)).
			To(Succeed(), "failed to create fraction pods")

		if throttled.Load() == throttledBefore {
			pause = 0
			continue
		}
		pause = min(max(2*pause, fractionBatchPauseStart), fractionBatchPauseMax)
		GinkgoLogr.Info("Pod creation was throttled by the API server, pausing before the next batch",
			"created", created+batch, "total", podCount, "throttledCreates", throttled.Load(), "pause", pause.String())
		sleepOrDone(ctx, pause)
	}
	if throttled.Load() > 0 {
		GinkgoLogr.Info("Pod creation finished after API server throttling", "throttledCreates", throttled.Load())
	}
}

func createFractionPodBatch(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue, podCount, memoryMiB int,
	throttled *atomic.Int64,
) error {
	semaphore := make(chan struct{}, fractionCreateConcurrency)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for range podCount {
		wg.Add(1)
		semaphore <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			if err := createWithBackoff(ctx, testCtx.ControllerClient, newFractionPod(testQueue, memoryMiB), throttled); err != nil {
				lock.Lock()
				creationError = errors.Join(creationError, err)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	return creationError
}

func createWithBackoff(
	ctx context.Context, client runtimeClient.Client, pod *v1.Pod, throttled *atomic.Int64,
) error {
	delay := fractionCreateBackoffStart
	for attempt := 0; ; attempt++ {
		err := client.Create(ctx, pod)
		if err == nil || apierrors.IsAlreadyExists(err) {
			return nil
		}
		if attempt == fractionCreateRetries || ctx.Err() != nil {
			return err
		}
		if apierrors.IsTooManyRequests(err) {
			throttled.Add(1)
		}
		sleepOrDone(ctx, delay)
		delay = min(2*delay, fractionCreateBackoffMax)
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func reservationPodCount(ctx context.Context, testCtx *testcontext.TestContext) int {
	pods := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(constant.KaiReservationNamespace),
		runtimeClient.HasLabels{constants.GPUGroup},
	)).To(Succeed())
	return len(pods.Items)
}

// bindRequestCreationTimes maps pod name to the creation time of its BindRequest, which is when the scheduler decided
// the allocation. Only metadata is fetched to keep the poll cheap at tens of thousands of pods.
func bindRequestCreationTimes(
	ctx context.Context, g Gomega, testCtx *testcontext.TestContext, namespace string,
) map[string]time.Time {
	bindRequests := &metav1.PartialObjectMetadataList{}
	bindRequests.SetGroupVersionKind(schedulingv1alpha2.GroupVersion.WithKind("BindRequestList"))
	g.Expect(testCtx.ControllerClient.List(ctx, bindRequests, runtimeClient.InNamespace(namespace))).To(Succeed())
	times := make(map[string]time.Time, len(bindRequests.Items))
	for i := range bindRequests.Items {
		times[bindRequests.Items[i].Name] = bindRequests.Items[i].CreationTimestamp.Time
	}
	return times
}

// fractionsSaturation splits the cluster's GPUs evenly across the tiers and submits exactly enough fractional pods to
// fill them. It waits until the scheduler decided on every pod (allocated or unschedulable) and every allocated pod is
// bound. Reaching less than 100% is allowed; the achieved utilization is reported.
func fractionsSaturation(
	ctx context.Context, testCtx *testcontext.TestContext, tiers []fractionTier, nodes int, gpuMemoryMiB int64,
) {
	capacityGPUs := nodes * gpusPerNode
	expectedPods := 0

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	for _, tier := range tiers {
		podCount := capacityGPUs / len(tiers) * tier.fractionsPerGPU
		createFractionPods(ctx, testCtx, tier.queue, podCount, fractionMemoryMiB(gpuMemoryMiB, tier.fractionsPerGPU))
		expectedPods += podCount
	}

	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)

	var allocated []allocatedFractionPod
	unschedulable := 0
	Eventually(func(g Gomega) {
		allocated, unschedulable = nil, 0
		total, unbound := 0, 0
		for _, tier := range tiers {
			namespace := queue.GetConnectedNamespaceToQueue(tier.queue)
			pods := &v1.PodList{}
			g.Expect(testCtx.ControllerClient.List(ctx, pods, runtimeClient.InNamespace(namespace),
				runtimeClient.MatchingLabels{testconfig.GetConfig().QueueLabelKey: tier.queue.Name})).To(Succeed())
			allocatedAt := bindRequestCreationTimes(ctx, g, testCtx, namespace)
			total += len(pods.Items)
			for _, pod := range pods.Items {
				if createdAt, hasBindRequest := allocatedAt[pod.Name]; hasBindRequest {
					allocated = append(allocated, allocatedFractionPod{pod, tier.fractionsPerGPU, createdAt})
					if !rd.IsPodScheduled(&pod) {
						unbound++
					}
				} else if rd.IsPodUnschedulable(&pod) {
					unschedulable++
				}
			}
		}
		g.Expect(total).To(Equal(expectedPods), "not all pods were created")
		g.Expect(len(allocated)+unschedulable).To(Equal(expectedPods),
			"%d of %d pods have no scheduling decision yet", expectedPods-len(allocated)-unschedulable, expectedPods)
		g.Expect(unbound).To(BeZero(), "%d of %d allocated pods are not bound yet", unbound, len(allocated))
	}, maxFlowTimeoutMinutes*time.Minute, podsPollIntervalSeconds*time.Second).Should(Succeed())
	Expect(allocated).NotTo(BeEmpty(), "the scheduler allocated no pods")

	var lastAllocatedAt, lastScheduledAt time.Time
	var allocatedGPUs float64
	boundPods := make([]v1.Pod, 0, len(allocated))
	bindLags := make([]float64, 0, len(allocated))
	for i := range allocated {
		scheduledAt, err := getPodScheduledTime(&allocated[i].pod)
		Expect(err).NotTo(HaveOccurred())
		if allocated[i].allocatedAt.After(lastAllocatedAt) {
			lastAllocatedAt = allocated[i].allocatedAt
		}
		if scheduledAt.After(lastScheduledAt) {
			lastScheduledAt = scheduledAt
		}
		// BindRequest and condition timestamps have 1s resolution, so lag can read slightly off.
		bindLags = append(bindLags, math.Max(scheduledAt.Sub(allocated[i].allocatedAt).Seconds(), 0))
		allocatedGPUs += 1 / float64(allocated[i].fractionsPerGPU)
		boundPods = append(boundPods, allocated[i].pod)
	}
	sort.Float64s(bindLags)

	groups, err := validateFractionPacking(boundPods, gpuMemoryMiB)
	Expect(err).NotTo(HaveOccurred())
	Expect(reservationPodCount(ctx, testCtx)).To(Equal(groups),
		"reservation pods must match the GPU groups in use")

	details := map[string]interface{}{
		"nodes":                nodes,
		"pods":                 expectedPods,
		"allocated_pods":       len(allocated),
		"unschedulable_pods":   unschedulable,
		"gpu_utilization_pct":  allocatedGPUs / float64(capacityGPUs) * 100,
		"allocation_seconds":   math.Max(lastAllocatedAt.Sub(startTime).Seconds(), 0),
		"scheduling_seconds":   math.Max(lastScheduledAt.Sub(startTime).Seconds(), 0),
		"bind_lag_p50_seconds": percentile(bindLags, 50),
		"bind_lag_p95_seconds": percentile(bindLags, 95),
		"bind_lag_max_seconds": percentile(bindLags, 100),
	}
	Expect(writeTestResults(saturationAllocationScenarioName, true, details)).To(Succeed())
	Expect(writeTestResults(saturationSchedulingScenarioName, true, details)).To(Succeed())
}
