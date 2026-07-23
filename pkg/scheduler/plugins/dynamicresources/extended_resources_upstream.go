// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Functions in this file are ported from:
// k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources/extendeddynamicresources.go
//
// Signatures are adapted to use *extendedresourcecache.ExtendedResourceCache in place of
// fwk.DeviceClassResolver, and to drop klog.Logger and *stateData parameters that are
// not needed in KAI's scheduling context.
//
// When upgrading Kubernetes, replace this file wholesale with the updated upstream logic.

package dynamicresources

import (
	"fmt"
	"slices"
	"sort"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/deviceclass/extendedresourcecache"
)

func createRequestsAndMappings(
	pod *v1.Pod,
	extendedResources map[v1.ResourceName]int64,
	dbc *extendedresourcecache.ExtendedResourceCache,
) ([]resourceapi.DeviceRequest, []v1.ContainerExtendedResourceRequest) {
	containers := append(pod.Spec.InitContainers, pod.Spec.Containers...) //nolint:gocritic
	longLived, shortLived := partitionContainerIndices(containers, len(pod.Spec.InitContainers))

	resourceNames := make([]v1.ResourceName, 0, len(extendedResources))
	for r := range extendedResources {
		resourceNames = append(resourceNames, r)
	}
	slices.Sort(resourceNames)

	var deviceRequests []resourceapi.DeviceRequest
	var mappings []v1.ContainerExtendedResourceRequest

	for _, rName := range resourceNames {
		dc := dbc.GetDeviceClass(rName)
		if dc == nil {
			continue
		}

		longLivedRequests := make([]*resourceapi.DeviceRequest, len(containers))
		var longLivedMappings []v1.ContainerExtendedResourceRequest
		for _, i := range longLived {
			req, mps := createResourceRequestAndMappings(i, &containers[i], rName, dc.Name, nil)
			longLivedRequests[i] = req
			longLivedMappings = append(longLivedMappings, mps...)
		}

		var maxShortLived *resourceapi.DeviceRequest
		var shortLivedMappings []v1.ContainerExtendedResourceRequest
		shortLivedNames := map[string]bool{}
		for _, i := range shortLived {
			req, mps := createResourceRequestAndMappings(i, &containers[i], rName, dc.Name, longLivedRequests[i:])
			if req != nil {
				shortLivedNames[req.Name] = true
				if maxShortLived == nil || maxShortLived.Exactly.Count < req.Exactly.Count {
					maxShortLived = req
				}
			}
			shortLivedMappings = append(shortLivedMappings, mps...)
		}
		if maxShortLived != nil && len(shortLivedNames) > 1 {
			delete(shortLivedNames, maxShortLived.Name)
			for i := range shortLivedMappings {
				if shortLivedNames[shortLivedMappings[i].RequestName] {
					shortLivedMappings[i].RequestName = maxShortLived.Name
				}
			}
		}

		if maxShortLived != nil {
			deviceRequests = append(deviceRequests, *maxShortLived)
		}
		for _, req := range longLivedRequests {
			if req != nil {
				deviceRequests = append(deviceRequests, *req)
			}
		}
		mappings = append(mappings, longLivedMappings...)
		mappings = append(mappings, shortLivedMappings...)
	}

	sort.Slice(deviceRequests, func(i, j int) bool { return deviceRequests[i].Name < deviceRequests[j].Name })
	return deviceRequests, mappings
}

func partitionContainerIndices(containers []v1.Container, numInitContainers int) ([]int, []int) {
	var longLived, shortLived []int
	for i, c := range containers {
		isInit := i < numInitContainers
		isSidecar := c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways
		if isInit && !isSidecar {
			shortLived = append(shortLived, i)
		} else {
			longLived = append(longLived, i)
		}
	}
	return longLived, shortLived
}

func createResourceRequestAndMappings(
	containerIndex int,
	container *v1.Container,
	rName v1.ResourceName,
	className string,
	reusableRequests []*resourceapi.DeviceRequest,
) (*resourceapi.DeviceRequest, []v1.ContainerExtendedResourceRequest) {
	if container.Resources.Requests == nil {
		return nil, nil
	}
	rQuant, ok := container.Resources.Requests[rName]
	if !ok {
		return nil, nil
	}
	crq, ok := (&rQuant).AsInt64()
	if !ok || crq == 0 {
		return nil, nil
	}

	var mappings []v1.ContainerExtendedResourceRequest
	sum := int64(0)
	for _, r := range reusableRequests {
		if r != nil {
			sum += r.Exactly.Count
			mappings = append(mappings, v1.ContainerExtendedResourceRequest{
				ContainerName: container.Name,
				ResourceName:  rName.String(),
				RequestName:   r.Name,
			})
			if sum >= crq {
				return nil, mappings
			}
		}
	}

	keys := make([]string, 0, len(container.Resources.Requests))
	for k := range container.Resources.Requests {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	ridx := 0
	for j, k := range keys {
		if k == rName.String() {
			ridx = j
			break
		}
	}

	reqName := fmt.Sprintf("container-%d-request-%d", containerIndex, ridx)
	deviceReq := resourceapi.DeviceRequest{
		Name: reqName,
		Exactly: &resourceapi.ExactDeviceRequest{
			DeviceClassName: className,
			AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
			Count:           crq - sum,
		},
	}
	mappings = append(mappings, v1.ContainerExtendedResourceRequest{
		ContainerName: container.Name,
		ResourceName:  rName.String(),
		RequestName:   reqName,
	})
	return &deviceReq, mappings
}
