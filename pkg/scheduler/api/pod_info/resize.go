// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
)

// IsResizeDeferred returns true when the kubelet reported the pod's in-place resize as
// Deferred for the current resize generation. A condition left over from a previous
// generation is ignored.
func (pi *PodInfo) IsResizeDeferred() bool {
	if pi.Pod == nil {
		return false
	}
	for _, condition := range pi.Pod.Status.Conditions {
		if condition.Type != v1.PodResizePending {
			continue
		}
		if condition.Status != v1.ConditionTrue || condition.Reason != v1.PodReasonDeferred {
			return false
		}
		return condition.ObservedGeneration == 0 || condition.ObservedGeneration == pi.Pod.Generation
	}
	return false
}

// DeferredResizeDelta returns the resources the kubelet still needs to allocate for the
// pod's deferred resize: per resizable resource (CPU, memory), the spec request minus the
// actual request from container statuses, clamped at zero, summed over regular containers.
// Returns nil when the pod has no deferred resize.
func (pi *PodInfo) DeferredResizeDelta() v1.ResourceList {
	if !pi.IsResizeDeferred() {
		return nil
	}

	actualByContainer := map[string]v1.ResourceList{}
	for _, containerStatus := range pi.Pod.Status.ContainerStatuses {
		if containerStatus.Resources != nil {
			actualByContainer[containerStatus.Name] = containerStatus.Resources.Requests
		} else if containerStatus.AllocatedResources != nil {
			actualByContainer[containerStatus.Name] = containerStatus.AllocatedResources
		}
	}

	delta := v1.ResourceList{}
	for _, container := range pi.Pod.Spec.Containers {
		actual := actualByContainer[container.Name]
		for _, resourceName := range []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory} {
			specRequest, found := container.Resources.Requests[resourceName]
			if !found {
				continue
			}
			actualRequest, found := actual[resourceName]
			if !found {
				// Unknown actual allocation - assume the spec is allocated rather than inflate the delta.
				continue
			}
			specRequest.Sub(actualRequest)
			if specRequest.Sign() <= 0 {
				continue
			}
			total := delta[resourceName]
			total.Add(specRequest)
			delta[resourceName] = total
		}
	}

	if len(delta) == 0 {
		return nil
	}
	return delta
}
