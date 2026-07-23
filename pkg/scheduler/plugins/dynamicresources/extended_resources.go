// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	resourcehelper "k8s.io/component-helpers/resource"
	"k8s.io/dynamic-resource-allocation/deviceclass/extendedresourcecache"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
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
// We scan raw pod container requests rather than the resource vector because some
// DRA-backed resources (e.g. nvidia.com/gpu at GPUIndex) live below PodsIndex and
// would be silently skipped by a vector-index scan starting at PodsIndex+1.
func podExtendedResourcesNeedingDRA(
	task *pod_info.PodInfo,
	nodeInfo *node_info.NodeInfo,
	dbc *extendedresourcecache.ExtendedResourceCache,
) map[v1.ResourceName]int64 {
	// PodRequests aggregates correctly: sum for regular/sidecar containers,
	// max for non-restartable init containers (which run sequentially).
	podReqs := resourcehelper.PodRequests(task.Pod, resourcehelper.PodResourcesOptions{})
	allocatable := nodeInfo.Node.Status.Allocatable
	result := make(map[v1.ResourceName]int64)
	for rName, rQuant := range podReqs {
		if dbc.GetDeviceClass(rName) == nil {
			continue
		}
		// If the node advertises device-plugin capacity for this resource it is
		// not a DRA-only dimension on this node; skip.
		if allocAmt, ok := allocatable[rName]; ok && !allocAmt.IsZero() {
			continue
		}
		crq, ok := rQuant.AsInt64()
		if !ok || crq <= 0 {
			continue
		}
		result[rName] = crq
	}
	return result
}
