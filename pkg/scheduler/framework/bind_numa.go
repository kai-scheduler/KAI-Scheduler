// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// numaPlacementToZones translates a task's internal, index-based NUMAPlacement into the durable,
// zone-id-based form carried on the BindRequest. This is the persistence boundary for the write
// direction: the scheduler reasons in zone indices in-cycle, and only here — where the per-cycle
// node topology that defines the index↔id mapping is in scope — does it resolve indices to the
// stable NUMA-node zone ids. Returns nil when the task has no placement or the node has no topology.
func numaPlacementToZones(pod *pod_info.PodInfo, node *node_info.NodeInfo) []schedulingv1alpha2.NUMAZonePlacement {
	if pod == nil || len(pod.NUMAPlacement) == 0 || node == nil || node.NumaTopology == nil {
		return nil
	}

	zones := make([]schedulingv1alpha2.NUMAZonePlacement, 0, len(pod.NUMAPlacement))
	for _, placement := range pod.NUMAPlacement {
		id, ok := node.NumaTopology.ZoneID(placement.ZoneIndex)
		if !ok {
			continue
		}
		zones = append(zones, schedulingv1alpha2.NUMAZonePlacement{Zone: id, Amount: placement.Amount})
	}
	return zones
}
