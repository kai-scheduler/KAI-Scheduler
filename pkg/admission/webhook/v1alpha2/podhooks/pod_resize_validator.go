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

// memoryLimitBytesPerUnit converts a Queue Memory.Limit (in megabytes) to bytes.
const memoryLimitBytesPerUnit = 1_000_000

// PodResizeValidator is an admission.Handler that enforces best-effort queue quota
// on pods/resize requests. It checks that the resource delta of the resize would
// not push any queue on the pod's hierarchy over its configured limit (all workloads)
// or over its deserved quota for non-preemptible workloads.
type PodResizeValidator struct {
	kubeClient    client.Client
	schedulerName string
	decoder       admission.Decoder
}

// NewPodResizeValidator creates a PodResizeValidator. The scheme is used to decode
// pod objects from the admission request.
func NewPodResizeValidator(kubeClient client.Client, scheme *runtime.Scheme, schedulerName string) *PodResizeValidator {
	return &PodResizeValidator{
		kubeClient:    kubeClient,
		schedulerName: schedulerName,
		decoder:       admission.NewDecoder(scheme),
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

	// Old effective = max(spec, enacted, allocated) ignoring the infeasible condition
	// (infeasible marks a previous-generation target, not the current occupancy).
	oldEffective := specPodRequests(oldPod)
	newEffective := specPodRequests(newPod)

	delta := subResourceList(newEffective, oldEffective)
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

		if err := checkQueueCapacity(queue, delta, isPreemptible, oldPod, pg); err != nil {
			return err
		}

		queueName = queue.Spec.ParentQueue
	}

	return nil
}

// specPodRequests sums spec.Containers[*].Resources.Requests across all containers.
// For the webhook context, effective old = spec (what the kubelet has currently committed)
// and effective new = proposed spec (what the resize targets).
func specPodRequests(pod *corev1.Pod) corev1.ResourceList {
	total := corev1.ResourceList{}
	for _, c := range pod.Spec.Containers {
		for name, qty := range c.Resources.Requests {
			sum := total[name]
			sum.Add(qty)
			total[name] = sum
		}
	}
	return total
}

// subResourceList computes max(proposed - current, 0) per resource.
func subResourceList(proposed, current corev1.ResourceList) corev1.ResourceList {
	zero := resource.MustParse("0")
	delta := corev1.ResourceList{}
	for name, qty := range proposed {
		cur := current[name]
		diff := qty.DeepCopy()
		diff.Sub(cur)
		if diff.Cmp(zero) > 0 {
			delta[name] = diff
		}
	}
	return delta
}

func checkQueueCapacity(
	queue *v2.Queue,
	delta corev1.ResourceList,
	isPreemptible bool,
	pod *corev1.Pod,
	pg *v2alpha2.PodGroup,
) error {
	if queue.Spec.Resources == nil {
		return nil
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

	if deltaCPU, ok := delta[corev1.ResourceCPU]; ok && res.CPU.Quota > 0 {
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

	if deltaMem, ok := delta[corev1.ResourceMemory]; ok && res.Memory.Quota > 0 {
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
