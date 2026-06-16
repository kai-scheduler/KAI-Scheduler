// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	v1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// requestUnits decomposes a task into the alignment units the kubelet Topology
// Manager hints for, under the node's scope: one unit for the whole pod (pod
// scope) or one per concurrently-running container (container scope). Each unit
// is a request the evaluator places on NUMA zone(s); the evaluator intersects it
// with the node's topology-aware resources, so non-aligned resources drop out.
func requestUnits(task *pod_info.PodInfo, scope node_info.TopologyManagerScope) []v1.ResourceList {
	if scope == node_info.TopologyScopePod {
		return []v1.ResourceList{toAmounts(resourcehelper.PodRequests(task.Pod, resourcehelper.PodResourcesOptions{}))}
	}
	return containerUnits(task.Pod)
}

// containerUnits returns one request per concurrently-running container: the
// regular containers plus native sidecars (restartable init containers). Ordinary
// init containers run serially before the app containers and are not modeled here
// (see follow-ups); the common single-container and sidecar cases are exact.
func containerUnits(pod *v1.Pod) []v1.ResourceList {
	units := make([]v1.ResourceList, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		if c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways {
			units = append(units, toAmounts(c.Resources.Requests))
		}
	}
	for i := range pod.Spec.Containers {
		units = append(units, toAmounts(pod.Spec.Containers[i].Resources.Requests))
	}
	return units
}

func toAmounts(list v1.ResourceList) v1.ResourceList {
	amounts := make(v1.ResourceList, len(list))
	for name, qty := range list {
		amounts[name] = qty.DeepCopy()
	}
	return amounts
}
