// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
	resourcehelpers "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

var resizableResources = []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}

func IsDeferredResizeTask(task *PodInfo) bool {
	if task == nil || task.Pod == nil || task.Status != pod_status.Running {
		return false
	}
	pod := task.Pod
	return resourcehelpers.IsPodResizeDeferred(pod) && hasPendingUpsize(pod)
}

// NewResizeStandIn returns a pending stand-in for a deferred resize task. It requests nothing,
// as KEP-5836's request minus cached footprint: the node already charges the task at its target size.
func NewResizeStandIn(task *PodInfo) *PodInfo {
	standIn := task.Clone()
	standIn.Status = pod_status.Pending
	standIn.IsResizeStandIn = true
	standIn.ResReqVector = make(resource_info.ResourceVector, len(task.ResReqVector))
	standIn.GpuRequirement = *resource_info.NewGpuResourceRequirement()
	return standIn
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
