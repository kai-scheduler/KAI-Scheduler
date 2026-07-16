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
	if pod == nil || len(pod.NUMAPlacement) == 0 || node == nil || node.NumaTopology == nil {
		return nil
	}

	zones := make([]schedulingv1alpha2.NUMAZonePlacement, 0, len(pod.NUMAPlacement))
	for _, placement := range pod.NUMAPlacement {
		id, ok := node.NumaTopology.ZoneID(placement.ZoneIndex)
		if !ok {
			log.InfraLogger.Errorf("Failed to get zone ID for placement %v for pod %s on node %s", placement, pod.Name, node.Name)
			continue
		}
		zones = append(zones, schedulingv1alpha2.NUMAZonePlacement{Zone: id, Amount: placement.Amount})
	}
	return zones
}
