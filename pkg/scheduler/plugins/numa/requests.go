// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	v1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// requestUnits decomposes a task into the alignment units the kubelet Topology Manager hints for,
// under the node's scope. It returns the concurrently-running units, which are charged against the
// per-zone ledger, and the serial init-container units, which must each be alignable on their own
// but are not accumulated (an ordinary init container completes and frees its resources before the
// app containers run). The evaluator intersects each unit with the node's topology-aware resources,
// so non-aligned resources drop out.
func requestUnits(task *pod_info.PodInfo, scope node_info.TopologyManagerScope) (concurrent, serial []v1.ResourceList) {
	if scope == node_info.TopologyScopePod {
		return []v1.ResourceList{toAmounts(resourcehelper.PodRequests(task.Pod, resourcehelper.PodResourcesOptions{}))}, nil
	}
	return containerUnits(task.Pod)
}

// containerUnits splits a pod into container-scope alignment units. Native sidecars (restartable
// init containers) keep running alongside the app containers, so they are concurrent and
// accumulated. An ordinary init container runs serially before the app containers and frees its
// resources first, so it is returned as a serial unit: checked for alignability on its own, never
// accumulated into the concurrent set.
func containerUnits(pod *v1.Pod) (concurrent, serial []v1.ResourceList) {
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		if c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways {
			concurrent = append(concurrent, toAmounts(c.Resources.Requests))
		} else {
			serial = append(serial, toAmounts(c.Resources.Requests))
		}
	}
	for i := range pod.Spec.Containers {
		concurrent = append(concurrent, toAmounts(pod.Spec.Containers[i].Resources.Requests))
	}
	return concurrent, serial
}

func toAmounts(list v1.ResourceList) v1.ResourceList {
	amounts := make(v1.ResourceList, len(list))
	for name, qty := range list {
		amounts[name] = qty.DeepCopy()
	}
	return amounts
}
