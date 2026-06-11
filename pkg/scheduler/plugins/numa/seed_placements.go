// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"encoding/json"
	"sort"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// seedPlacements translates each already-placed pod's persisted NUMA placement into the internal
// index-based NUMAPlacement, so virtual eviction can credit the pod's actual zones. The placement is
// resolved by the pod_info layer (observed > BindRequest > predicted; see PodInfo.NUMAPlacementRecord)
// and carried as durable zone ids — this only maps those ids to the per-cycle topology indices. A pod
// with no record, or whose record names a zone the node no longer reports, is left unaccounted — v1
// never guesses a zone. Only pods the plugin would handle are seeded, keeping allocate/deallocate
// charging symmetric.
//
// Seeding targets the canonical task objects on the PodGroupInfos, NOT NodeInfo.PodInfos: the node
// holds *clones* (NodeInfo.addTask deep-copies), while preemption/reclaim evict the job task
// (utils.GetVictimsQueue iterates job.GetAllPodsMap()), so the DeallocateFunc credit reads the job
// task's placement. The node copy re-syncs from the job task on Evict (UpdateTask re-clones).
func (pp *numaPlugin) seedPlacements(ssn *framework.Session) {
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range job.GetAllPodsMap() {
			if len(task.NUMAPlacement) > 0 || task.NodeName == "" {
				continue
			}
			node := ssn.ClusterInfo.Nodes[task.NodeName]
			if node == nil || !pp.shouldHandle(task, node.NumaTopology) {
				continue
			}
			raw, ok := task.Pod.Annotations[commonconstants.NumaPlacementObserved]
			if !ok {
				continue
			}
			var record []schedulingv1alpha2.NUMAZonePlacement
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				continue
			}
			task.NUMAPlacement = placementFromRecord(record, node.NumaTopology)
		}
	}
}

// placementFromRecord maps a persisted (zone-id-based) NUMA placement record to the internal index
// form, ordered by zone index (stable for the eviction dedup). Returns nil if any zone id is absent
// from the current topology — a partial placement would under-credit, so the whole record is treated
// as unknown.
func placementFromRecord(record []schedulingv1alpha2.NUMAZonePlacement, topo *node_info.NumaTopology) pod_info.NUMAPlacement {
	if len(record) == 0 {
		return nil
	}
	placement := make(pod_info.NUMAPlacement, 0, len(record))
	for _, zone := range record {
		idx, ok := topo.ZoneIndexByID(zone.Zone)
		if !ok {
			return nil
		}
		placement = append(placement, pod_info.ZonePlacement{ZoneIndex: idx, Amount: zone.Amount})
	}
	sort.Slice(placement, func(i, j int) bool { return placement[i].ZoneIndex < placement[j].ZoneIndex })
	return placement
}
