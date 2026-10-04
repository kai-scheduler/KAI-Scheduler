// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"math"

	v1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/component-helpers/resource"

	commonpod "github.com/kai-scheduler/KAI-scheduler/pkg/common/pod"
	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

type admissionUnit struct {
	container    *v1.Container
	request      resource_info.ResourceVector
	ordinaryInit bool
}

type podNumaRequests struct {
	podScope resource_info.ResourceVector
	units    []admissionUnit
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
	for name := range podReq {
		if commonresources.IsMemoryResource(name) {
			delete(podReq, name)
		}
	}
	for _, container := range longRunningContainers(pod) {
		for name, quantity := range container.Resources.Requests {
			if commonresources.IsMemoryResource(name) {
				amount := podReq[name]
				amount.Add(quantity)
				podReq[name] = amount
			}
		}
	}
	podVec := resource_info.NewResourceVectorFromResourceList(podReq, vectorMap)
	setCPUMilli(podVec, cpuIdx, podGuaranteedCPUMilli(pod))
	return &podNumaRequests{podScope: podVec, units: admissionUnits(pod, vectorMap)}
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
	if !commonpod.IsGuaranteed(pod) {
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

func admissionUnits(pod *v1.Pod, vectorMap *resource_info.ResourceVectorMap) []admissionUnit {
	units := make([]admissionUnit, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	appendContainer := func(container *v1.Container, ordinary bool) {
		request := resource_info.NewResourceVectorFromResourceList(container.Resources.Requests, vectorMap)
		if ordinary {
			for name := range container.Resources.Requests {
				if commonresources.IsMemoryResource(name) {
					index := vectorMap.GetIndex(name)
					if index >= 0 {
						request[index] = 0
					}
				}
			}
		}
		setCPUMilli(request, vectorMap.GetIndex(v1.ResourceCPU), guaranteedCPUMilli(pod, container))
		units = append(units, admissionUnit{container: container, request: request, ordinaryInit: ordinary})
	}
	for index := range pod.Spec.InitContainers {
		container := &pod.Spec.InitContainers[index]
		appendContainer(container, !isNativeSidecar(container))
	}
	for index := range pod.Spec.Containers {
		appendContainer(&pod.Spec.Containers[index], false)
	}
	return units
}

func longRunningContainers(pod *v1.Pod) []*v1.Container {
	containers := make([]*v1.Container, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	for index := range pod.Spec.InitContainers {
		container := &pod.Spec.InitContainers[index]
		if isNativeSidecar(container) {
			containers = append(containers, container)
		}
	}
	for index := range pod.Spec.Containers {
		containers = append(containers, &pod.Spec.Containers[index])
	}
	return containers
}
