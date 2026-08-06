// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonpodgroup "github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup"
)

var resizeLog = logf.Log.WithName("pod-resize-validator")

func isPodResizeInfeasible(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodResizePending {
			return c.Status == corev1.ConditionTrue && c.Reason == corev1.PodReasonInfeasible
		}
	}
	return false
}

// memoryLimitBytesPerUnit converts a Queue Memory.Limit (in megabytes) to bytes.
const memoryLimitBytesPerUnit = 1_000_000

// PodResizeValidator is an admission.Handler that enforces best-effort queue quota
// on pods/resize requests. It checks that the resource delta of the resize would
// not push any queue on the pod's hierarchy over its configured limit (all workloads)
// or over its deserved quota for non-preemptible workloads.
type PodResizeValidator struct {
	kubeClient                 client.Client
	schedulerName              string
	decoder                    admission.Decoder
	validateQuota              bool
	blockUpsizeOnBoundedQueues bool
}

// NewPodResizeValidator creates a PodResizeValidator. The scheme is used to decode
// pod objects from the admission request.
func NewPodResizeValidator(
	kubeClient client.Client,
	scheme *runtime.Scheme,
	schedulerName string,
	validateQuota bool,
	blockUpsizeOnBoundedQueues bool,
) *PodResizeValidator {
	return &PodResizeValidator{
		kubeClient:                 kubeClient,
		schedulerName:              schedulerName,
		decoder:                    admission.NewDecoder(scheme),
		validateQuota:              validateQuota,
		blockUpsizeOnBoundedQueues: blockUpsizeOnBoundedQueues,
	}
}

func (v *PodResizeValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	newPod := &corev1.Pod{}
	if err := v.decoder.DecodeRaw(req.Object, newPod); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode new pod: %w", err))
	}

	if newPod.Spec.SchedulerName != v.schedulerName {
		return admission.Allowed("")
	}

	oldPod := &corev1.Pod{}
	if err := v.decoder.DecodeRaw(req.OldObject, oldPod); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode old pod: %w", err))
	}

	if err := v.validateResize(ctx, oldPod, newPod); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

func (v *PodResizeValidator) validateResize(ctx context.Context, oldPod, newPod *corev1.Pod) error {
	if !v.validateQuota {
		return nil
	}

	pgName, ok := oldPod.Annotations[commonconstants.PodGroupAnnotationForPod]
	if !ok || pgName == "" {
		return nil
	}

	pg := &v2alpha2.PodGroup{}
	if err := v.kubeClient.Get(ctx, client.ObjectKey{Namespace: oldPod.Namespace, Name: pgName}, pg); err != nil {
		resizeLog.Error(err, "failed to get PodGroup", "namespace", oldPod.Namespace, "name", pgName)
		return nil // best-effort: allow on lookup failure
	}

	isPreemptible, err := v.resolvePreemptibility(ctx, pg)
	if err != nil {
		resizeLog.Error(err, "failed to resolve preemptibility", "podgroup", pgName)
		isPreemptible = true // conservative: treat unknown as preemptible
	}

	delta := podResizeDelta(oldPod, newPod)
	if len(delta) == 0 {
		return nil // downsize or no change
	}

	queueName := pg.Spec.Queue
	for queueName != "" {
		queue := &v2.Queue{}
		if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: queueName}, queue); err != nil {
			resizeLog.Error(err, "failed to get queue", "queue", queueName)
			return nil // best-effort: allow on lookup failure
		}

		if err := checkQueueCapacity(queue, delta, isPreemptible, v.blockUpsizeOnBoundedQueues, oldPod, pg); err != nil {
			return err
		}

		queueName = queue.Spec.ParentQueue
	}

	return nil
}

// podResizeDelta computes the net per-resource increase this resize introduces
// relative to what the queue already accounts for.
//
// The queue's Status.Allocated is a pod-level sum of per-container effective
// requests, so the delta is computed at the same granularity:
//
//  1. For each regular container and each restartable init container (sidecar),
//     accumulate three pod-level sums: newSpecSum, oldSpecSum, effectiveOldSum.
//  2. Skip a resource if its pod-level spec is unchanged (newSpecSum[r] ==
//     oldSpecSum[r]) — that resource was not part of this resize request and
//     must not generate spurious delta even when an earlier infeasible attempt
//     left an unresolved stale spec.
//  3. For changed resources, delta = max(0, newSpecSum[r] - effectiveOldSum[r]).
//     Pod-level aggregation naturally handles CPU/memory moved between containers
//     (redistribution produces zero net delta at the pod level).
//
// Effective old baseline per container:
//   - If old pod has a current Infeasible condition: max(enacted, allocated).
//     The infeasible spec was never committed by the kubelet, so the queue only
//     reflects enacted/allocated.
//   - Otherwise (normal / Deferred / InProgress): max(spec, enacted, allocated),
//     matching what the scheduler charges for those states.
//   - Falls back to old spec when no ContainerStatus is available.
func podResizeDelta(oldPod, newPod *corev1.Pod) corev1.ResourceList {
	oldInfeasible := isPodResizeInfeasible(oldPod)

	statusByName := make(map[string]*corev1.ContainerStatus, len(oldPod.Status.ContainerStatuses))
	for i := range oldPod.Status.ContainerStatuses {
		statusByName[oldPod.Status.ContainerStatuses[i].Name] = &oldPod.Status.ContainerStatuses[i]
	}
	oldByName := make(map[string]*corev1.Container, len(oldPod.Spec.Containers))
	for i := range oldPod.Spec.Containers {
		oldByName[oldPod.Spec.Containers[i].Name] = &oldPod.Spec.Containers[i]
	}

	initStatusByName := make(map[string]*corev1.ContainerStatus, len(oldPod.Status.InitContainerStatuses))
	for i := range oldPod.Status.InitContainerStatuses {
		initStatusByName[oldPod.Status.InitContainerStatuses[i].Name] = &oldPod.Status.InitContainerStatuses[i]
	}
	oldInitByName := make(map[string]*corev1.Container, len(oldPod.Spec.InitContainers))
	for i := range oldPod.Spec.InitContainers {
		oldInitByName[oldPod.Spec.InitContainers[i].Name] = &oldPod.Spec.InitContainers[i]
	}

	newSpecSum := corev1.ResourceList{}
	oldSpecSum := corev1.ResourceList{}
	effectiveOldSum := corev1.ResourceList{}

	for i := range newPod.Spec.Containers {
		accumulateDeltaSums(&newPod.Spec.Containers[i], oldByName, statusByName, oldInfeasible, newSpecSum, oldSpecSum, effectiveOldSum)
	}
	for i := range newPod.Spec.InitContainers {
		c := &newPod.Spec.InitContainers[i]
		if c.RestartPolicy == nil || *c.RestartPolicy != corev1.ContainerRestartPolicyAlways {
			continue
		}
		accumulateDeltaSums(c, oldInitByName, initStatusByName, oldInfeasible, newSpecSum, oldSpecSum, effectiveOldSum)
	}

	zero := resource.MustParse("0")
	delta := corev1.ResourceList{}
	for r, newQty := range newSpecSum {
		if oldQty, ok := oldSpecSum[r]; ok && newQty.Cmp(oldQty) == 0 {
			continue // resource not changed by this resize at the pod level
		}
		effectiveOld := effectiveOldSum[r]
		diff := newQty.DeepCopy()
		diff.Sub(effectiveOld)
		if diff.Cmp(zero) > 0 {
			delta[r] = diff
		}
	}
	return delta
}

// accumulateDeltaSums accumulates one container's contribution into three pod-level
// sums. The effective old baseline follows the queue's own accounting:
//   - infeasible old pod: max(enacted, allocated)
//   - otherwise:          max(old_spec, enacted, allocated)
func accumulateDeltaSums(
	newC *corev1.Container,
	oldByName map[string]*corev1.Container,
	statusByName map[string]*corev1.ContainerStatus,
	oldInfeasible bool,
	newSpecSum, oldSpecSum, effectiveOldSum corev1.ResourceList,
) {
	oldC := oldByName[newC.Name]
	cs := statusByName[newC.Name]

	for resName, newQty := range newC.Resources.Requests {
		cur := newSpecSum[resName]
		cur.Add(newQty)
		newSpecSum[resName] = cur

		var oldSpecQty resource.Quantity
		if oldC != nil {
			oldSpecQty = oldC.Resources.Requests[resName]
		}
		cur = oldSpecSum[resName]
		cur.Add(oldSpecQty)
		oldSpecSum[resName] = cur

		effectiveOld := oldSpecQty
		if cs != nil {
			candidates := []resource.Quantity{}
			if !oldInfeasible {
				candidates = append(candidates, oldSpecQty)
			}
			if cs.Resources != nil {
				if enacted, ok := cs.Resources.Requests[resName]; ok {
					candidates = append(candidates, enacted)
				}
			}
			if alloc, ok := cs.AllocatedResources[resName]; ok {
				candidates = append(candidates, alloc)
			}
			if len(candidates) > 0 {
				best := candidates[0]
				for _, q := range candidates[1:] {
					if q.Cmp(best) > 0 {
						best = q
					}
				}
				effectiveOld = best
			}
		}
		cur = effectiveOldSum[resName]
		cur.Add(effectiveOld)
		effectiveOldSum[resName] = cur
	}
}

func checkQueueCapacity(
	queue *v2.Queue,
	delta corev1.ResourceList,
	isPreemptible bool,
	blockUpsizeOnBoundedQueues bool,
	pod *corev1.Pod,
	pg *v2alpha2.PodGroup,
) error {
	if queue.Spec.Resources == nil {
		return nil
	}

	if blockUpsizeOnBoundedQueues {
		if err := checkBlockUpsizeOnBoundedQueue(queue, delta, isPreemptible, pod, pg); err != nil {
			return err
		}
	}

	// Check hard limit for all workloads.
	if err := checkLimit(queue, delta, pod, pg); err != nil {
		return err
	}

	// Check deserved quota for non-preemptible workloads.
	if !isPreemptible {
		if err := checkNonPreemptibleQuota(queue, delta, pod, pg); err != nil {
			return err
		}
	}

	return nil
}

// checkBlockUpsizeOnBoundedQueue rejects any upsize when a queue has a finite
// limit (all workloads) or a finite quota (non-preemptible workloads), regardless
// of current allocation. This prevents races between concurrent resize requests.
func checkBlockUpsizeOnBoundedQueue(
	queue *v2.Queue,
	delta corev1.ResourceList,
	isPreemptible bool,
	pod *corev1.Pod,
	pg *v2alpha2.PodGroup,
) error {
	res := queue.Spec.Resources

	if _, ok := delta[corev1.ResourceCPU]; ok {
		if res.CPU.Limit >= 0 {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) CPU upsize not permitted on queue %s with finite CPU limit (%.0fm)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, res.CPU.Limit,
			)
		}
		if !isPreemptible && res.CPU.Quota >= 0 {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) non-preemptible CPU upsize not permitted on queue %s with finite CPU quota (%.0fm)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, res.CPU.Quota,
			)
		}
	}

	if _, ok := delta[corev1.ResourceMemory]; ok {
		if res.Memory.Limit >= 0 {
			limitBytes := int64(res.Memory.Limit * memoryLimitBytesPerUnit)
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) memory upsize not permitted on queue %s with finite memory limit (%d bytes)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, limitBytes,
			)
		}
		if !isPreemptible && res.Memory.Quota >= 0 {
			quotaBytes := int64(res.Memory.Quota * memoryLimitBytesPerUnit)
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) non-preemptible memory upsize not permitted on queue %s with finite memory quota (%d bytes)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, quotaBytes,
			)
		}
	}

	return nil
}

func checkLimit(queue *v2.Queue, delta corev1.ResourceList, pod *corev1.Pod, pg *v2alpha2.PodGroup) error {
	res := queue.Spec.Resources

	if deltaCPU, ok := delta[corev1.ResourceCPU]; ok && res.CPU.Limit >= 0 {
		allocated := queue.Status.Allocated[corev1.ResourceCPU]
		newAlloc := float64(allocated.MilliValue() + deltaCPU.MilliValue())
		if newAlloc > res.CPU.Limit {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) CPU upsize would push queue %s Allocated (%.0fm + %.0fm) over limit (%.0fm)",
				pod.Namespace, pod.Name, pg.Name, queue.Name,
				float64(allocated.MilliValue()), float64(deltaCPU.MilliValue()), res.CPU.Limit,
			)
		}
	}

	if deltaMem, ok := delta[corev1.ResourceMemory]; ok && res.Memory.Limit >= 0 {
		limitBytes := int64(res.Memory.Limit * memoryLimitBytesPerUnit)
		allocated := queue.Status.Allocated[corev1.ResourceMemory]
		newAllocBytes := allocated.Value() + deltaMem.Value()
		if newAllocBytes > limitBytes {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) memory upsize would push queue %s Allocated (%d + %d bytes) over limit (%d bytes)",
				pod.Namespace, pod.Name, pg.Name, queue.Name,
				allocated.Value(), deltaMem.Value(), limitBytes,
			)
		}
	}

	return nil
}

func checkNonPreemptibleQuota(queue *v2.Queue, delta corev1.ResourceList, pod *corev1.Pod, pg *v2alpha2.PodGroup) error {
	res := queue.Spec.Resources

	if deltaCPU, ok := delta[corev1.ResourceCPU]; ok && res.CPU.Quota >= 0 {
		allocated := queue.Status.AllocatedNonPreemptible[corev1.ResourceCPU]
		newAlloc := float64(allocated.MilliValue() + deltaCPU.MilliValue())
		if newAlloc > res.CPU.Quota {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) non-preemptible CPU upsize would push queue %s AllocatedNonPreemptible (%.0fm + %.0fm) over quota (%.0fm)",
				pod.Namespace, pod.Name, pg.Name, queue.Name,
				float64(allocated.MilliValue()), float64(deltaCPU.MilliValue()), res.CPU.Quota,
			)
		}
	}

	if deltaMem, ok := delta[corev1.ResourceMemory]; ok && res.Memory.Quota >= 0 {
		quotaBytes := int64(res.Memory.Quota * memoryLimitBytesPerUnit)
		allocated := queue.Status.AllocatedNonPreemptible[corev1.ResourceMemory]
		newAllocBytes := allocated.Value() + deltaMem.Value()
		if newAllocBytes > quotaBytes {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) non-preemptible memory upsize would push queue %s AllocatedNonPreemptible (%d + %d bytes) over quota (%d bytes)",
				pod.Namespace, pod.Name, pg.Name, queue.Name,
				allocated.Value(), deltaMem.Value(), quotaBytes,
			)
		}
	}

	return nil
}

func (v *PodResizeValidator) resolvePreemptibility(ctx context.Context, pg *v2alpha2.PodGroup) (bool, error) {
	preemptibility := pg.Spec.Preemptibility
	if preemptibility == v2alpha2.Preemptible || preemptibility == v2alpha2.NonPreemptible {
		return preemptibility == v2alpha2.Preemptible, nil
	}

	priority, err := v.resolvePriority(ctx, pg)
	if err != nil {
		return true, err
	}

	result := commonpodgroup.CalculatePreemptibility("", priority)
	return result == v2alpha2.Preemptible, nil
}

func (v *PodResizeValidator) resolvePriority(ctx context.Context, pg *v2alpha2.PodGroup) (int32, error) {
	if pg.Spec.PriorityClassName == "" {
		return 0, nil
	}

	pc := &schedulingv1.PriorityClass{}
	if err := v.kubeClient.Get(ctx, client.ObjectKey{Name: pg.Spec.PriorityClassName}, pc); err != nil {
		return 0, fmt.Errorf("get PriorityClass %s: %w", pg.Spec.PriorityClassName, err)
	}
	return pc.Value, nil
}
