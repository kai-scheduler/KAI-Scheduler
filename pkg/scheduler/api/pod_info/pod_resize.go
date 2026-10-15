// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
	resourcehelpers "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

// PodResizePreemptionDisabled is set by the kubelet (KEP-5836) when the node opts out of
// preemption for deferred resizes. Not yet defined in k8s.io/api.
const PodResizePreemptionDisabled v1.PodConditionType = "PodResizePreemptionDisabled"

var resizableResources = []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}

// IsDeferredResizeTask reports whether a running task has a kubelet-Deferred in-place resize
// that still asks for more cpu or memory than the kubelet has allocated or enacted. Any other
// status means the session is evicting, moving or still starting the pod.
func IsDeferredResizeTask(task *PodInfo) bool {
	if task == nil || task.Pod == nil || task.Status != pod_status.Running {
		return false
	}
	pod := task.Pod
	if !resourcehelpers.IsPodResizeDeferred(pod) || isResizePreemptionDisabled(pod) {
		return false
	}
	return hasPendingUpsize(pod)
}

func isResizePreemptionDisabled(pod *v1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == PodResizePreemptionDisabled {
			return c.Status == v1.ConditionTrue
		}
	}
	return false
}

// hasPendingUpsize guards against a stale Deferred condition left after a later downsize.
func hasPendingUpsize(pod *v1.Pod) bool {
	statuses := make(map[string]*v1.ContainerStatus,
		len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	for i := range pod.Status.ContainerStatuses {
		statuses[pod.Status.ContainerStatuses[i].Name] = &pod.Status.ContainerStatuses[i]
	}
	for i := range pod.Status.InitContainerStatuses {
		statuses[pod.Status.InitContainerStatuses[i].Name] = &pod.Status.InitContainerStatuses[i]
	}
	containers := pod.Spec.Containers
	for _, container := range pod.Spec.InitContainers {
		if container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways {
			containers = append(containers[:len(containers):len(containers)], container)
		}
	}
	for _, container := range containers {
		status, found := statuses[container.Name]
		if !found {
			continue
		}
		for _, name := range resizableResources {
			desired, found := container.Resources.Requests[name]
			if !found {
				continue
			}
			current := status.AllocatedResources[name]
			if status.Resources != nil {
				if actual, found := status.Resources.Requests[name]; found && actual.Cmp(current) > 0 {
					current = actual
				}
			}
			if desired.Cmp(current) > 0 {
				return true
			}
		}
	}
	return false
}
