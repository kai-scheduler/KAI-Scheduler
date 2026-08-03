// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"math"

	v1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
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

// numaRequestsFor builds and caches the task's NUMA requests on first use. Not safe for concurrent
// use: the predicate path is assumed serial.
func (pp *numaPlugin) numaRequestsFor(task *pod_info.PodInfo, vectorMap *resource_info.ResourceVectorMap) *podNumaRequests {
	if pp.numaRequestCache == nil {
		pp.numaRequestCache = map[common_info.PodID]*podNumaRequests{}
	}
	if reqs, ok := pp.numaRequestCache[task.UID]; ok {
		return reqs
	}
	reqs := buildNumaRequests(task.Pod, vectorMap)
	pp.numaRequestCache[task.UID] = reqs
	return reqs
}

func buildNumaRequests(pod *v1.Pod, vectorMap *resource_info.ResourceVectorMap) *podNumaRequests {
	cpuIdx := vectorMap.GetIndex(v1.ResourceCPU)

	podReq := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
	podVec := resource_info.NewResourceVectorFromResourceList(podReq, vectorMap)
	setCPUMilli(podVec, cpuIdx, podGuaranteedCPUMilli(pod))
	reqs := &podNumaRequests{podScope: []resource_info.ResourceVector{podVec}}

	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		vec := resource_info.NewResourceVectorFromResourceList(c.Resources.Requests, vectorMap)
		setCPUMilli(vec, cpuIdx, guaranteedCPUMilli(pod, c))
		if isNativeSidecar(c) {
			reqs.concurrent = append(reqs.concurrent, vec)
		} else {
			reqs.serial = append(reqs.serial, vec)
		}
	}
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		vec := resource_info.NewResourceVectorFromResourceList(c.Resources.Requests, vectorMap)
		setCPUMilli(vec, cpuIdx, guaranteedCPUMilli(pod, c))
		reqs.concurrent = append(reqs.concurrent, vec)
	}
	return reqs
}

func isNativeSidecar(c *v1.Container) bool {
	return c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways
}

func setCPUMilli(vec resource_info.ResourceVector, cpuIdx int, milli float64) {
	if cpuIdx >= 0 && cpuIdx < len(vec) {
		vec[cpuIdx] = milli
	}
}

// guaranteedCPUMilli mirrors the kubelet's staticPolicy.guaranteedCPUs: a container is allocated
// exclusive, NUMA-aligned CPUs only in a Guaranteed pod and only for a whole number of CPUs. A
// container requesting fractional CPU stays in the shared pool and constrains no NUMA zone, so
// summing its request (as resourcehelper.PodRequests does) would over-constrain the pod.
func guaranteedCPUMilli(pod *v1.Pod, c *v1.Container) float64 {
	if pod.Status.QOSClass != v1.PodQOSGuaranteed {
		return 0
	}
	q := c.Resources.Requests[v1.ResourceCPU]
	if q.Value()*1000 != q.MilliValue() {
		return 0
	}
	return float64(q.MilliValue())
}

// podGuaranteedCPUMilli mirrors the kubelet's staticPolicy.podGuaranteedCPUs: the init-peak vs
// long-running-sum shape of PodRequests, but with each container's non-aligned CPU zeroed.
func podGuaranteedCPUMilli(pod *v1.Pod) float64 {
	initPeak, restartableInit := 0.0, 0.0
	for i := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[i]
		cpu := guaranteedCPUMilli(pod, c)
		if isNativeSidecar(c) {
			restartableInit += cpu
		} else if restartableInit+cpu > initPeak {
			initPeak = restartableInit + cpu
		}
	}

	longRunning := restartableInit
	for i := range pod.Spec.Containers {
		longRunning += guaranteedCPUMilli(pod, &pod.Spec.Containers[i])
	}
	return math.Max(longRunning, initPeak)
}
