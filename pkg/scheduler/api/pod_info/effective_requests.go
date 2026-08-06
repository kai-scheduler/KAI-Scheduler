// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
)

// effectivePodContainerRequests returns the effective resource requests for each
// regular container, applying the upstream KEP-1287 in-place resize model:
//
//   - normal / Deferred / InProgress: max(spec.Requests, cs.Resources.Requests, cs.AllocatedResources)
//   - Infeasible (PodResizePending=Infeasible): max(cs.Resources.Requests, cs.AllocatedResources)
//
// If ContainerStatus.Resources is nil (no resize in progress), returns spec.Requests unchanged.
func effectivePodContainerRequests(pod *v1.Pod) map[string]v1.ResourceList {
	infeasible := isPodResizeInfeasible(pod)

	byName := make(map[string]*v1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for i := range pod.Status.ContainerStatuses {
		byName[pod.Status.ContainerStatuses[i].Name] = &pod.Status.ContainerStatuses[i]
	}

	result := make(map[string]v1.ResourceList, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		cs, found := byName[c.Name]
		if !found || cs.Resources == nil {
			result[c.Name] = c.Resources.Requests
			continue
		}
		if infeasible {
			result[c.Name] = maxResourceList(cs.Resources.Requests, cs.AllocatedResources)
		} else {
			result[c.Name] = maxResourceList(c.Resources.Requests, cs.Resources.Requests, cs.AllocatedResources)
		}
	}
	return result
}

// isPodResizeInfeasible returns true when the kubelet has reported the current
// resize request as infeasible for the pod's current generation.
func isPodResizeInfeasible(pod *v1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == v1.PodResizePending {
			return c.Status == v1.ConditionTrue && c.Reason == v1.PodReasonInfeasible
		}
	}
	return false
}

// maxResourceList returns an element-wise max over the supplied ResourceLists.
func maxResourceList(lists ...v1.ResourceList) v1.ResourceList {
	result := make(v1.ResourceList)
	for _, list := range lists {
		for name, qty := range list {
			if cur, ok := result[name]; !ok || qty.Cmp(cur) > 0 {
				result[name] = qty.DeepCopy()
			}
		}
	}
	return result
}

