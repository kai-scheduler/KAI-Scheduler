// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"context"

	discovery "k8s.io/client-go/discovery"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// CollectRawObjects lists the scheduler's raw objects and deep-copies them.
// The copy is safe to retain after the call returns: several informer objects are
// mutated in place during a scheduling cycle. A nil lister returns nil.
func CollectRawObjects(lister data_lister.DataLister) *RawKubernetesObjects {
	if lister == nil {
		return nil
	}

	return &RawKubernetesObjects{
		Pods:                   deepCopySlice(listOrEmpty(lister.ListPods, "Error getting raw pods")),
		Nodes:                  deepCopySlice(listOrEmpty(lister.ListNodes, "Error getting raw nodes")),
		Queues:                 deepCopySlice(listOrEmpty(lister.ListQueues, "Error getting raw queues")),
		PodGroups:              deepCopySlice(listOrEmpty(lister.ListPodGroups, "Error getting raw pod groups")),
		BindRequests:           deepCopySlice(listOrEmpty(lister.ListBindRequests, "Error getting raw bind requests")),
		PriorityClasses:        deepCopySlice(listOrEmpty(lister.ListPriorityClasses, "Error getting raw priority classes")),
		ConfigMaps:             deepCopySlice(listOrEmpty(lister.ListConfigMaps, "Error getting raw config maps")),
		PersistentVolumes:      deepCopySlice(listOrEmpty(lister.ListPersistentVolumes, "Error getting raw persistent volumes")),
		PersistentVolumeClaims: deepCopySlice(listOrEmpty(lister.ListPersistentVolumeClaims, "Error getting raw persistent volume claims")),
		CSIStorageCapacities:   deepCopySlice(listOrEmpty(lister.ListCSIStorageCapacities, "Error getting raw CSI storage capacities")),
		StorageClasses:         deepCopySlice(listOrEmpty(lister.ListStorageClasses, "Error getting raw storage classes")),
		CSIDrivers:             deepCopySlice(listOrEmpty(lister.ListCSIDrivers, "Error getting raw CSI drivers")),
		ResourceClaims:         deepCopySlice(listOrEmpty(lister.ListResourceClaims, "Error getting raw resource claims")),
		ResourceSlices:         deepCopySlice(listOrEmpty(lister.ListResourceSlices, "Error getting raw resource slices")),
		DeviceClasses:          deepCopySlice(listOrEmpty(lister.ListDeviceClasses, "Error getting raw device classes")),
		Topologies:             deepCopySlice(listOrEmpty(lister.ListTopologies, "Error getting raw topologies")),
		NodeResourceTopologies: deepCopySlice(listOrEmpty(lister.ListNodeResourceTopologies, "Error getting raw node resource topologies")),
	}
}

// CollectDiscovery reads the API server version and resource list.
// Failures are logged and left nil so a caller can mark the copy partial.
func CollectDiscovery(ctx context.Context, discoveryClient discovery.DiscoveryInterface) *DiscoverySnapshot {
	discoverySnapshot := &DiscoverySnapshot{}
	if discoveryClient == nil {
		return discoverySnapshot
	}

	var err error
	discoverySnapshot.ServerVersion, err = getServerVersion(ctx, discoveryClient)
	if err != nil {
		log.InfraLogger.V(2).Warnf("Failed to snapshot server version: %v", err)
		discoverySnapshot.ServerVersion = nil
	}

	_, discoverySnapshot.Resources, err = discoveryClient.ServerGroupsAndResources()
	if err != nil {
		log.InfraLogger.V(2).Warnf("Failed to snapshot server resources: %v", err)
		discoverySnapshot.Resources = nil
	}

	return discoverySnapshot
}

func listOrEmpty[T any](list func() ([]T, error), errorMessage string) []T {
	values, err := list()
	if err != nil {
		log.InfraLogger.Errorf("%s: %v", errorMessage, err)
		return []T{}
	}
	if values == nil {
		return []T{}
	}
	return values
}

func deepCopySlice[T interface{ DeepCopy() T }](in []T) []T {
	out := make([]T, 0, len(in))
	for _, item := range in {
		if any(item) == nil {
			continue
		}
		out = append(out, item.DeepCopy())
	}
	return out
}
