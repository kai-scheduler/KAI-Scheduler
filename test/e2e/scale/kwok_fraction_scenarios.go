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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	waitutils "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/api/kai/v1/common"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
)

const (
	fractionPodsPerNode        = 14 // 7000 pods at 500 nodes
	fractionDefaultGPUMemory   = 81920
	fractionContainerName      = "ubuntu-container"
	fractionCreateConcurrency  = 100
	fractionChurnWindow        = 10 * time.Minute
	fractionP95LimitSeconds    = 5.0
	fractionReservationTimeout = 5 * time.Minute
	fractionConfigTimeout      = 5 * time.Minute

	burstScenarioName = "Fractions allocation burst"
	churnScenarioName = "Fractions churn latency"
)

// fractionMixEntry requests memory equal to 1/fractionsPerGPU of a GPU for share percent of the pods.
type fractionMixEntry struct {
	fractionsPerGPU int
	share           int
}

var defaultFractionMix = []fractionMixEntry{{fractionsPerGPU: 8, share: 50}, {fractionsPerGPU: 2, share: 50}}

var fractionPodRequirement = v1.ResourceRequirements{
	Requests: v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("100m"),
		v1.ResourceMemory: resource.MustParse("128Mi"),
	},
}

func fractionPodCount(nodes int) int {
	return nodes * fractionPodsPerNode
}

func fractionMemoryMiB(gpuMemoryMiB, fractionsPerGPU int) int {
	return gpuMemoryMiB / fractionsPerGPU
}

// fractionAssignments returns fractionsPerGPU for each of total pods; rounding remainder goes to the first entry.
func fractionAssignments(total int, mix []fractionMixEntry) []int {
	assignments := make([]int, 0, total)
	for i, entry := range mix {
		count := total * entry.share / 100
		if i == 0 {
			count = total
			for _, other := range mix[1:] {
				count -= total * other.share / 100
			}
		}
		for range count {
			assignments = append(assignments, entry.fractionsPerGPU)
		}
	}
	return assignments
}

func fractionGPUsNeeded(assignments []int) float64 {
	needed := 0.0
	for _, perGPU := range assignments {
		needed += 1 / float64(perGPU)
	}
	return needed
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
	waitutils.ForKAIConfigStatusOKWithTimeout(ctx, testCtx.ControllerClient, fractionConfigTimeout)
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

func newFractionPod(
	testQueue *v2.Queue, memoryMiB int, labels map[string]string,
) *v1.Pod {
	pod := rd.CreatePodObject(testQueue, fractionPodRequirement)
	pod.Annotations[fractionMemoryAnnotationKey()] = fmt.Sprintf("%dMi", memoryMiB)
	for key, value := range labels {
		pod.Labels[key] = value
	}
	addKWOKTaintsAndAffinity(&pod.Spec)
	return pod
}

func createFractionPods(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	assignments []int, gpuMemoryMiB int64, labels map[string]string,
) {
	semaphore := make(chan struct{}, fractionCreateConcurrency)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for _, perGPU := range assignments {
		wg.Add(1)
		semaphore <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			pod := newFractionPod(testQueue, fractionMemoryMiB(int(gpuMemoryMiB), perGPU), labels)
			if err := rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, pod); err != nil {
				lock.Lock()
				creationError = errors.Join(creationError, err)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	Expect(creationError).NotTo(HaveOccurred(), "failed to create fraction pods")
}

func reservationPodCount(ctx context.Context, testCtx *testcontext.TestContext) int {
	pods := &v1.PodList{}
	Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(constant.KaiReservationNamespace),
		runtimeClient.HasLabels{constants.GPUGroup},
	)).To(Succeed())
	return len(pods.Items)
}

func mixDescription(mix []fractionMixEntry) string {
	description := ""
	for i, entry := range mix {
		if i > 0 {
			description += ","
		}
		description += fmt.Sprintf("%d%%x1/%d", entry.share, entry.fractionsPerGPU)
	}
	return description
}

func fractionsAllocationBurst(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	nodes int, gpuMemoryMiB int64, mix []fractionMixEntry,
) {
	assignments := fractionAssignments(fractionPodCount(nodes), mix)
	Expect(fractionGPUsNeeded(assignments)).To(BeNumerically("<=", nodes*gpusPerNode),
		"fraction mix does not fit the cluster")
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}

	schedulerconfig.DisableScheduler(ctx, testCtx)
	defer schedulerconfig.EnableScheduler(ctx, testCtx)

	createFractionPods(ctx, testCtx, testQueue, assignments, gpuMemoryMiB, batchLabels)
	startTime := time.Now()
	schedulerconfig.EnableScheduler(ctx, testCtx)
	endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, len(assignments))

	pods := listBatchPods(ctx, testCtx, testQueue, batchLabels)
	groups, err := validateFractionPacking(pods, gpuMemoryMiB)
	Expect(err).NotTo(HaveOccurred())
	Expect(reservationPodCount(ctx, testCtx)).To(Equal(groups),
		"reservation pods must match the GPU groups in use")

	duration := endTime.Sub(startTime).Seconds()
	Expect(writeTestResults(burstScenarioName, true, map[string]interface{}{
		"nodes":            nodes,
		"pods":             len(assignments),
		"fraction_mix":     mixDescription(mix),
		"gpu_groups":       groups,
		"duration_seconds": duration,
		"pods_per_second":  float64(len(assignments)) / math.Max(duration, 1),
	})).To(Succeed())
}

func fractionsChurnLatency(
	ctx context.Context, testCtx *testcontext.TestContext, testQueue *v2.Queue,
	nodes int, gpuMemoryMiB int64, mix []fractionMixEntry,
) {
	assignments := fractionAssignments(fractionPodCount(nodes), mix)
	Expect(fractionGPUsNeeded(assignments)).To(BeNumerically("<=", nodes*gpusPerNode),
		"fraction mix does not fit the cluster")
	batchLabels := map[string]string{distributedJobBatchLabel: utils.GenerateRandomK8sName(10)}
	// Pods are completed by the KWOK stage selecting this label, freeing fractions for later arrivals.
	podLabels := map[string]string{"burst-test": "true", distributedJobBatchLabel: batchLabels[distributedJobBatchLabel]}

	perSecond := int(math.Ceil(float64(len(assignments)) / fractionChurnWindow.Seconds()))
	startTime := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for created := 0; created < len(assignments); created += perSecond {
		createFractionPods(ctx, testCtx, testQueue,
			assignments[created:min(created+perSecond, len(assignments))], gpuMemoryMiB, podLabels)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			Fail("context cancelled while creating churn pods")
		}
	}
	endTime := waitForBatchToSchedule(ctx, testCtx, testQueue, batchLabels, len(assignments))

	pods := listBatchPods(ctx, testCtx, testQueue, batchLabels)
	// Pod creation and scheduled timestamps have 1s resolution, so latencies carry up to 1s of error.
	latencies := make([]float64, 0, len(pods))
	for i := range pods {
		scheduledTime, err := getPodScheduledTime(&pods[i])
		Expect(err).NotTo(HaveOccurred())
		latencies = append(latencies, math.Max(scheduledTime.Sub(pods[i].CreationTimestamp.Time).Seconds(), 0))
	}
	sort.Float64s(latencies)
	p95 := percentile(latencies, 95)
	Expect(p95).To(BeNumerically("<=", fractionP95LimitSeconds), "p95 scheduling latency too high")

	Eventually(func(g Gomega) {
		g.Expect(reservationPodCount(ctx, testCtx)).To(BeZero(), "reservation pods leaked after all pods completed")
	}, fractionReservationTimeout, podsPollIntervalSeconds*time.Second).Should(Succeed())

	Expect(writeTestResults(churnScenarioName, true, map[string]interface{}{
		"nodes":               nodes,
		"pods":                len(assignments),
		"fraction_mix":        mixDescription(mix),
		"arrival_window_secs": fractionChurnWindow.Seconds(),
		"duration_seconds":    endTime.Sub(startTime).Seconds(),
		"p50_latency_seconds": percentile(latencies, 50),
		"p95_latency_seconds": p95,
		"p99_latency_seconds": percentile(latencies, 99),
		"max_latency_seconds": percentile(latencies, 100),
	})).To(Succeed())
}
