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
	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	commonpodgroup "github.com/kai-scheduler/KAI-scheduler/pkg/common/podgroup"
)

var resizeLog = logf.Log.WithName("pod-resize-validator")

// memoryLimitBytesPerUnit converts a Queue Memory.Limit (in megabytes) to bytes.
const memoryLimitBytesPerUnit = 1_000_000

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

	if !commonpod.IsAllocated(oldPod) {
		return nil
	}

	pgName, ok := oldPod.Annotations[commonconstants.PodGroupAnnotationForPod]
	if !ok || pgName == "" {
		return nil
	}

	pg := &v2alpha2.PodGroup{}
	if err := v.kubeClient.Get(ctx, client.ObjectKey{Namespace: oldPod.Namespace, Name: pgName}, pg); err != nil {
		resizeLog.Error(err, "failed to get PodGroup", "namespace", oldPod.Namespace, "name", pgName)
		return nil
	}

	isPreemptible, err := commonpodgroup.IsPreemptible(ctx, pg, v.kubeClient)
	if err != nil {
		resizeLog.Error(err, "failed to resolve preemptibility", "podgroup", pgName)
		isPreemptible = true
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
			return nil
		}

		if err := checkQueueCapacity(queue, delta, isPreemptible, v.blockUpsizeOnBoundedQueues, oldPod, pg); err != nil {
			return err
		}

		queueName = queue.Spec.ParentQueue
	}

	return nil
}

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

func checkCapacityBound(
	boundKind string,
	cpuBound, memBound float64,
	allocated corev1.ResourceList,
	allocatedLabel string,
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
