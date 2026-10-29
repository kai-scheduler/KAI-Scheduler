// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

// numaPlacementToZones translates a task's internal, index-based NUMAPlacement into the durable,
// zone-id-based form carried on the BindRequest. The order of Zones in the NRT CRD is not guaranteed.
// Returns nil when the task has no placement or the node has no topology.
func numaPlacementToZones(pod *pod_info.PodInfo, node *node_info.NodeInfo) []schedulingv1alpha2.NUMAZonePlacement {
	if pod == nil || len(pod.NUMAPlacement.Zones) == 0 || node == nil || node.NumaTopology == nil {
		return nil
	}

	zones := make([]schedulingv1alpha2.NUMAZonePlacement, 0, len(pod.NUMAPlacement.Zones))
	for _, placement := range pod.NUMAPlacement.Zones {
		id, ok := node.NumaTopology.ZoneID(placement.ZoneIndex)
		if !ok {
			log.InfraLogger.Errorf("Failed to get zone ID for placement %v for pod %s on node %s", placement, pod.Name, node.Name)
			continue
		}
		zones = append(zones, schedulingv1alpha2.NUMAZonePlacement{Zone: id, Amount: placement.Amount})
	}
	return zones
}

func numaPlacementToMemoryGroups(pod *pod_info.PodInfo, node *node_info.NodeInfo) []schedulingv1alpha2.NUMAMemoryGroupPlacement {
	if pod == nil || len(pod.NUMAPlacement.MemoryGroups) == 0 || node == nil || node.NumaTopology == nil {
		return nil
	}
	groups := make([]schedulingv1alpha2.NUMAMemoryGroupPlacement, 0, len(pod.NUMAPlacement.MemoryGroups))
	for _, group := range pod.NUMAPlacement.MemoryGroups {
		nodes := make([]string, 0, group.Mask.Width())
		for _, index := range group.Mask.Indices() {
			id, ok := node.NumaTopology.ZoneID(index)
			if !ok {
				log.InfraLogger.Errorf("Invalid memory mask %v for pod %s on node %s", group.Mask, pod.Name, node.Name)
				return nil
			}
			nodes = append(nodes, id)
		}
		groups = append(groups, schedulingv1alpha2.NUMAMemoryGroupPlacement{MemoryNodes: nodes, Amount: group.Amount.DeepCopy()})
	}
	return groups
}
