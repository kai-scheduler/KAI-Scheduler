// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	resourcehelpers "k8s.io/component-helpers/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	v2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	commonpodgroup "github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup"
)

var resizeLog = logf.Log.WithName("pod-resize-validator")

// memoryLimitBytesPerUnit converts a Queue Memory.Limit (in megabytes) to bytes.
const memoryLimitBytesPerUnit = 1_000_000

// PodResizeValidator enforces best-effort queue quota on pods/resize requests.
// It checks that the resource delta of the resize would not push any queue on
// the pod's hierarchy over its configured limit (all workloads) or over its
// deserved quota for non-preemptible workloads.
type PodResizeValidator struct {
	kubeClient                 client.Client
	schedulerName              string
	validateQuota              bool
	blockUpsizeOnBoundedQueues bool
}

var _ admission.Validator[*corev1.Pod] = &PodResizeValidator{}

func NewPodResizeValidator(
	kubeClient client.Client,
	schedulerName string,
	validateQuota bool,
	blockUpsizeOnBoundedQueues bool,
) *PodResizeValidator {
	return &PodResizeValidator{
		kubeClient:                 kubeClient,
		schedulerName:              schedulerName,
		validateQuota:              validateQuota,
		blockUpsizeOnBoundedQueues: blockUpsizeOnBoundedQueues,
	}
}

func (v *PodResizeValidator) ValidateUpdate(ctx context.Context, oldPod, newPod *corev1.Pod) (admission.Warnings, error) {
	if newPod.Spec.SchedulerName != v.schedulerName {
		return nil, nil
	}
	return nil, v.validateResize(ctx, oldPod, newPod)
}

// ValidateCreate and ValidateDelete never fire: the webhook is registered only
// for UPDATE operations on the pods/resize subresource.
func (v *PodResizeValidator) ValidateCreate(context.Context, *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

func (v *PodResizeValidator) ValidateDelete(context.Context, *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
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

	// Same resolution the podgroup-controller uses to populate
	// AllocatedNonPreemptible — the checker and the accountant must agree.
	isPreemptible, err := commonpodgroup.IsPreemptible(ctx, pg, v.kubeClient)
	if err != nil {
		resizeLog.Error(err, "failed to resolve preemptibility", "podgroup", pgName)
		isPreemptible = true // conservative: treat unknown as preemptible
	}

	delta := podResizeDelta(oldPod, newPod)
	if len(delta) == 0 {
		return nil
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
// relative to what the queue already accounts for. All aggregates use the same
// upstream helper as scheduler accounting, so the effectiveOld baseline matches
// what the queue charges by construction. A resource whose pod-level spec is
// unchanged is skipped: it is not part of this resize, and an unresolved
// Infeasible spec on it must not produce phantom delta.
func podResizeDelta(oldPod, newPod *corev1.Pod) corev1.ResourceList {
	specOnly := resourcehelpers.PodResourcesOptions{}
	withStatus := resourcehelpers.PodResourcesOptions{UseStatusResources: true}

	newSpec := resourcehelpers.AggregateContainerRequests(newPod, specOnly)
	oldSpec := resourcehelpers.AggregateContainerRequests(oldPod, specOnly)
	effectiveOld := resourcehelpers.AggregateContainerRequests(oldPod, withStatus)

	delta := corev1.ResourceList{}
	for resName, newQty := range newSpec {
		if oldQty, ok := oldSpec[resName]; ok && newQty.Cmp(oldQty) == 0 {
			continue
		}
		diff := newQty.DeepCopy()
		diff.Sub(effectiveOld[resName])
		if diff.Sign() > 0 {
			delta[resName] = diff
		}
	}
	return delta
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

	res := queue.Spec.Resources

	if err := checkCapacityBound("limit", res.CPU.Limit, res.Memory.Limit,
		queue.Status.Allocated, "Allocated", delta, queue, pod, pg); err != nil {
		return err
	}

	if !isPreemptible {
		if err := checkCapacityBound("quota", res.CPU.Quota, res.Memory.Quota,
			queue.Status.AllocatedNonPreemptible, "AllocatedNonPreemptible", delta, queue, pod, pg); err != nil {
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

// checkCapacityBound rejects the resize when adding delta to the given allocated
// pool would exceed a finite bound (-1 means unbounded).
func checkCapacityBound(
	boundKind string, // "limit" | "quota"
	cpuBound, memBound float64,
	allocated corev1.ResourceList,
	allocatedLabel string, // "Allocated" | "AllocatedNonPreemptible"
	delta corev1.ResourceList,
	queue *v2.Queue,
	pod *corev1.Pod,
	pg *v2alpha2.PodGroup,
) error {
	if deltaCPU, ok := delta[corev1.ResourceCPU]; ok && cpuBound >= 0 {
		alloc := allocated[corev1.ResourceCPU]
		if float64(alloc.MilliValue()+deltaCPU.MilliValue()) > cpuBound {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) CPU upsize would push queue %s %s (%dm + %dm) over %s (%.0fm)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, allocatedLabel,
				alloc.MilliValue(), deltaCPU.MilliValue(), boundKind, cpuBound,
			)
		}
	}

	if deltaMem, ok := delta[corev1.ResourceMemory]; ok && memBound >= 0 {
		boundBytes := int64(memBound * memoryLimitBytesPerUnit)
		alloc := allocated[corev1.ResourceMemory]
		if alloc.Value()+deltaMem.Value() > boundBytes {
			return fmt.Errorf(
				"resize rejected: pod %s/%s (PodGroup %s) memory upsize would push queue %s %s (%d + %d bytes) over %s (%d bytes)",
				pod.Namespace, pod.Name, pg.Name, queue.Name, allocatedLabel,
				alloc.Value(), deltaMem.Value(), boundKind, boundBytes,
			)
		}
	}

	return nil
}
