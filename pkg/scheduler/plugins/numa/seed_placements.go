// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"encoding/json"
	"sort"

	v1 "k8s.io/api/core/v1"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// seedPlacements reconstructs each already-placed pod's NUMA placement from its persisted, durable
// (zone-id-based) record and stamps it onto the task as the internal index-based NUMAPlacement, so
// virtual eviction can credit the pod's actual zones. The record is resolved here with precedence
// observed > BindRequest > predicted (resolvePlacementRecord), then translated to the per-cycle zone
// indices. A pod with no record, or whose record names a zone the node no longer reports, is left
// unaccounted — v1 never guesses a zone. Only pods the plugin would handle are seeded, keeping
// allocate/deallocate charging symmetric.
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
			record := resolvePlacementRecord(task.Pod, bindRequestZones(ssn, task.Pod))
			task.NUMAPlacement = placementFromRecord(record, node.NumaTopology)
		}
	}
}

// bindRequestZones returns the pod's predicted NUMA zones carried on its (non-failed) BindRequest, or
// nil. Read from the session's BindRequest map rather than PodInfo.BindRequest because the latter is
// dropped by PodInfo.Clone; the map is clone-independent.
func bindRequestZones(ssn *framework.Session, pod *v1.Pod) []schedulingv1alpha2.NUMAZonePlacement {
	bindRequest := ssn.ClusterInfo.BindRequests.GetBindRequestForPod(pod)
	if bindRequest == nil {
		return nil
	}
	return bindRequest.BindRequest.Spec.PredictedNUMAZones
}

// resolvePlacementRecord returns a pod's persisted (zone-id-based) NUMA placement, with precedence
// observed (agent-published, ground truth) > BindRequest (this cycle's freshest prediction, readable
// before the binder patches the pod) > predicted annotation (the binder-written durable form).
// Returns nil when none is present — v1 never guesses a zone.
func resolvePlacementRecord(pod *v1.Pod, bindRequestZones []schedulingv1alpha2.NUMAZonePlacement) []schedulingv1alpha2.NUMAZonePlacement {
	if observed, ok := parsePlacementAnnotation(pod, commonconstants.NumaPlacementObserved); ok {
		return observed
	}
	if len(bindRequestZones) > 0 {
		return bindRequestZones
	}
	if predicted, ok := parsePlacementAnnotation(pod, commonconstants.NumaPlacementPredicted); ok {
		return predicted
	}
	return nil
}

// parsePlacementAnnotation decodes a JSON-encoded []NUMAZonePlacement annotation. A missing
// annotation yields no record silently; a present-but-malformed one is logged at warning level (it
// likely means an incompatible NUMA agent).
func parsePlacementAnnotation(pod *v1.Pod, key string) ([]schedulingv1alpha2.NUMAZonePlacement, bool) {
	raw, ok := pod.Annotations[key]
	if !ok || raw == "" {
		return nil, false
	}
	var record []schedulingv1alpha2.NUMAZonePlacement
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		log.InfraLogger.Warningf("numa: ignoring malformed %s annotation on pod <%s/%s>: %v "+
			"(possible incompatible NUMA agent)", key, pod.Namespace, pod.Name, err)
		return nil, false
	}
	if len(record) == 0 {
		log.InfraLogger.Warningf("numa: ignoring empty %s annotation on pod <%s/%s> "+
			"(possible incompatible NUMA agent)", key, pod.Namespace, pod.Name)
		return nil, false
	}
	return record, true
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
