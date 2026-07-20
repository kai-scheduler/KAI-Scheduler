// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/dynamic-resource-allocation/deviceclass/extendedresourcecache"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// specialClaimInMemName is the placeholder name for an in-memory synthetic claim.
// Not a valid API name, so it cannot conflict with real claims.
const specialClaimInMemName = "<extended-resources>"

// hasDeviceClassMappedExtendedResources returns true when any container in the pod
// requests a resource that is mapped to a DeviceClass in dbc.
func hasDeviceClassMappedExtendedResources(pod *v1.Pod, dbc *extendedresourcecache.ExtendedResourceCache) bool {
	if dbc == nil {
		return false
	}
	containers := append(pod.Spec.InitContainers, pod.Spec.Containers...) //nolint:gocritic
	for i := range containers {
		for rName, rQuant := range containers[i].Resources.Requests {
			if !rQuant.IsZero() && dbc.GetDeviceClass(rName) != nil {
				return true
			}
		}
	}
	return false
}

// findExtendedResourceClaim returns the synthetic extended-resource claim already
// created for the pod (from a prior scheduling/binding cycle), or nil if none exists.
func findExtendedResourceClaim(pod *v1.Pod, manager ksf.SharedDRAManager) *resourceapi.ResourceClaim {
	claims, err := manager.ResourceClaims().List()
	if err != nil {
		return nil
	}
	for _, c := range claims {
		if c.Annotations[resourceapi.ExtendedResourceClaimAnnotation] != "true" {
			continue
		}
		for _, or_ := range c.OwnerReferences {
			if or_.Name == pod.Name && or_.UID == pod.UID && or_.Controller != nil && *or_.Controller {
				return c
			}
		}
	}
	return nil
}

// buildSpecialClaim creates the in-memory synthetic ResourceClaim for the pod.
func buildSpecialClaim(pod *v1.Pod) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    pod.Namespace,
			Name:         specialClaimInMemName,
			UID:          types.UID(uuid.NewUUID()),
			GenerateName: pod.Name + "-extended-resources-",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       pod.Name,
					UID:        pod.UID,
					Controller: ptr.To(true),
				},
			},
			Annotations: map[string]string{
				resourceapi.ExtendedResourceClaimAnnotation: "true",
			},
		},
		Spec: resourceapi.ResourceClaimSpec{},
	}
}

// podExtendedResourcesNeedingDRA returns the set of extended resources that need
// DRA allocation on the given node (i.e., they are DRA-backed AND the node has no
// device-plugin capacity for them).
func podExtendedResourcesNeedingDRA(
	task *pod_info.PodInfo,
	nodeInfo *node_info.NodeInfo,
	dbc *extendedresourcecache.ExtendedResourceCache,
) map[v1.ResourceName]int64 {
	result := make(map[v1.ResourceName]int64)
	for i := resource_info.PodsIndex + 1; i < len(task.ResReqVector); i++ {
		val := task.ResReqVector.Get(i)
		if val == 0 {
			continue
		}
		name := nodeInfo.VectorMap.ResourceAt(i)
		if dbc.GetDeviceClass(name) == nil {
			continue
		}
		if nodeInfo.AllocatableVector.Get(i) == 0 {
			result[name] = int64(val)
		}
	}
	return result
}

// createRequestsAndMappings builds DeviceRequests and ContainerMappings for the
// synthetic extended-resource claim. Ported from upstream extendeddynamicresources.go.
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
	sort.Slice(resourceNames, func(i, j int) bool { return resourceNames[i] < resourceNames[j] })

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

	// Determine sorted request index for deterministic name generation
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
