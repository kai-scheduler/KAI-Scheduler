// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// IsResizeDeferred reports whether the pod has an in-place resize (KEP-1287) that the
// kubelet has deferred because the node currently lacks the capacity to actuate it.
//
// Detection uses the GA (k8s 1.35) PodResizePending condition with reason Deferred.
// Resizes with reason Infeasible are intentionally excluded: an Infeasible resize can
// never be actuated even on an empty node, so evicting neighbours cannot help it.
func IsResizeDeferred(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodResizePending && condition.Reason == v1.PodReasonDeferred {
			return true
		}
	}
	return false
}

// sumContainerRequests returns the aggregate requests declared by the pod's regular
// containers - the desired state after an in-place resize is applied to the spec.
func sumContainerRequests(pod *v1.Pod) v1.ResourceList {
	total := v1.ResourceList{}
	for _, container := range pod.Spec.Containers {
		addResourceList(total, container.Resources.Requests)
	}
	return total
}

// sumContainerStatusResources returns the resources the kubelet has actually granted the
// pod's regular containers, read from status.containerStatuses[].resources. This is the
// realized counterpart to sumContainerRequests, and is only populated once the
// InPlacePodVerticalScaling feature is in effect on the node.
func sumContainerStatusResources(pod *v1.Pod) v1.ResourceList {
	total := v1.ResourceList{}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Resources == nil {
			continue
		}
		addResourceList(total, containerStatus.Resources.Requests)
	}
	return total
}

func addResourceList(into, from v1.ResourceList) {
	for name, quantity := range from {
		sum := into[name]
		sum.Add(quantity)
		into[name] = sum
	}
}

// resizeDeferredDeltaList returns the positive, per-resource growth of a deferred in-place resize:
// the desired requests (pod spec) minus the resources the kubelet has actually granted (pod
// status). It returns an empty list when the pod has no deferred resize or the resize grows
// nothing.
//
// The diff is taken over raw ResourceLists (rather than converted ResourceRequirements) so that a
// resource which does not change - notably an explicit "nvidia.com/gpu: 0" request - contributes a
// zero diff and is dropped, instead of being misread as a fractional device. The implicit "pods"
// resource is ignored, since an in-place resize does not change it.
func resizeDeferredDeltaList(pod *v1.Pod) v1.ResourceList {
	if !IsResizeDeferred(pod) {
		return nil
	}

	desired := sumContainerRequests(pod)
	actual := sumContainerStatusResources(pod)

	deltaList := v1.ResourceList{}
	for name, desiredQuantity := range desired {
		if name == v1.ResourcePods {
			continue
		}
		diff := desiredQuantity.DeepCopy()
		if actualQuantity, found := actual[name]; found {
			diff.Sub(actualQuantity)
		}
		if diff.Sign() > 0 {
			deltaList[name] = diff
		}
	}
	return deltaList
}

// ResizeDeferredDelta returns the additional resources that a deferred in-place resize needs freed
// on the pod's node, or nil when the pod has no deferred resize or the resize grows nothing (for
// example one that only lowers requests, or one the kubelet has already actuated).
func ResizeDeferredDelta(pod *v1.Pod) *resource_info.ResourceRequirements {
	deltaList := resizeDeferredDeltaList(pod)
	if len(deltaList) == 0 {
		return nil
	}

	delta := resource_info.RequirementsFromResourceList(deltaList)
	if delta.IsEmpty() {
		return nil
	}
	return delta
}

// regularContainerRequests returns the resource requests to charge the scheduler for the pod's
// regular containers. A pod with a kubelet-deferred in-place resize is charged at the resources
// actually granted (desired spec minus the resize delta), so the node reflects the pod's true
// current footprint while the growth is represented separately as a pending demand; the charge
// plus the delta always equals the desired request. Any other pod is charged its spec requests
// unchanged.
func regularContainerRequests(pod *v1.Pod) v1.ResourceList {
	requests := sumContainerRequests(pod)
	for name, delta := range resizeDeferredDeltaList(pod) {
		quantity := requests[name]
		quantity.Sub(delta)
		requests[name] = quantity
	}
	return requests
}

// IsResizeDeferred reports whether this task carries a kubelet-deferred in-place resize.
func (pi *PodInfo) IsResizeDeferred() bool {
	return pi.Pod != nil && IsResizeDeferred(pi.Pod)
}

// ResizeDeferredDelta returns the extra resources this task's deferred resize needs freed
// on its node, or nil when there is none. See the package-level ResizeDeferredDelta.
func (pi *PodInfo) ResizeDeferredDelta() *resource_info.ResourceRequirements {
	if pi.Pod == nil {
		return nil
	}
	return ResizeDeferredDelta(pi.Pod)
}

const resizeReservationSuffix = "-resize-reservation"

// ResizeReservationJobID is the deterministic PodGroup id of the synthetic reservation that stands
// in for a resizing pod's growth, derived from the resizing task's own job id.
func ResizeReservationJobID(resizingTask *PodInfo) common_info.PodGroupID {
	return common_info.PodGroupID(string(resizingTask.Job) + resizeReservationSuffix)
}

// NewResizeReservationTask builds the synthetic pending pod that represents the growth of a
// kubelet-deferred in-place resize: a single task requesting the resize delta, pinned to the
// resizing pod's node via required node affinity on the well-known hostname label, and flagged
// IsResizeReservation so it is never bound. It has no backing workload - it exists only to make
// the scheduler free room on the node, subject to the queue's quota and fairness. It returns nil
// when the pod is unassigned or its resize grows nothing.
func NewResizeReservationTask(resizingTask *PodInfo, vectorMap *resource_info.ResourceVectorMap) *PodInfo {
	if resizingTask.Pod == nil || resizingTask.NodeName == "" {
		return nil
	}
	deltaList := resizeDeferredDeltaList(resizingTask.Pod)
	if len(deltaList) == 0 {
		return nil
	}

	jobID := ResizeReservationJobID(resizingTask)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(string(resizingTask.UID) + resizeReservationSuffix),
			Name:      resizingTask.Name + resizeReservationSuffix,
			Namespace: resizingTask.Namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: string(jobID),
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Resources: v1.ResourceRequirements{Requests: deltaList},
			}},
			Affinity: requiredHostnameAffinity(resizingTask.NodeName),
		},
		Status: v1.PodStatus{Phase: v1.PodPending},
	}

	task := NewTaskInfo(pod, vectorMap)
	task.Job = jobID
	task.IsResizeReservation = true
	// A reservation is not a real pod; drop the implicit pods:1 that getPodResourceRequest adds so it
	// does not consume a pod-count slot on the node - only the resize delta should be charged.
	task.ResReqVector = resource_info.RequirementsFromResourceList(deltaList).ToVector(vectorMap)
	return task
}

// requiredHostnameAffinity pins a pod to a single node via required node affinity on the well-known
// kubernetes.io/hostname label, which every node carries.
func requiredHostnameAffinity(nodeName string) *v1.Affinity {
	return &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      v1.LabelHostname,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{nodeName},
					}},
				}},
			},
		},
	}
}
