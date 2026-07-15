// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	v1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// podNumaRequests is a task decomposed into the alignment units the kubelet Topology Manager hints
// for, as vectors. Pod scope: the whole pod is one request. Container scope: concurrent units (app
// containers + native sidecars, charged against a shared per-zone ledger) and serial units (ordinary
// init containers, each alignable on its own but never accumulated, since they free their resources
// before the app containers run).
type podNumaRequests struct {
	podScope   []resource_info.ResourceVector
	concurrent []resource_info.ResourceVector
	serial     []resource_info.ResourceVector
}

func (r *podNumaRequests) forScope(scope node_info.TopologyManagerScope) (concurrent, serial []resource_info.ResourceVector) {
	if scope == node_info.TopologyScopePod {
		return r.podScope, nil
	}
	return r.concurrent, r.serial
}

func buildNumaRequests(pod *v1.Pod, vectorMap *resource_info.ResourceVectorMap) *podNumaRequests {
	podReq := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
	reqs := &podNumaRequests{
		podScope: []resource_info.ResourceVector{resource_info.NewResourceVectorFromResourceList(podReq, vectorMap)},
	}

	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		vec := resource_info.NewResourceVectorFromResourceList(c.Resources.Requests, vectorMap)
		if c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways {
			reqs.concurrent = append(reqs.concurrent, vec)
		} else {
			reqs.serial = append(reqs.serial, vec)
		}
	}
	for i := range pod.Spec.Containers {
		reqs.concurrent = append(reqs.concurrent,
			resource_info.NewResourceVectorFromResourceList(pod.Spec.Containers[i].Resources.Requests, vectorMap))
	}
	return reqs
}
