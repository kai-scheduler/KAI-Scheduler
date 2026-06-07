// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"encoding/json"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	// NUMAPlacementObservedAnnotation carries a pod's actual NUMA placement, as
	// observed by the per-node placement agent (ground truth).
	NUMAPlacementObservedAnnotation = "kai.scheduler/numa-placement-observed"
	// NUMAPlacementPredictedAnnotation carries the scheduler's predicted NUMA
	// placement, written by the binder on commit (the placement record).
	NUMAPlacementPredictedAnnotation = "kai.scheduler/numa-placement-predicted"
)

// ZoneCharge is a task's charge on one NUMA zone: the zone id and the exact
// per-resource amount placed there.
type ZoneCharge struct {
	Zone   string          `json:"zone"`
	Amount v1.ResourceList `json:"amount,omitempty"`
}

// NUMAPlacement is a task's NUMA placement — its zone(s) and per-zone amounts. It
// is the NUMA analog of GPUGroups: framework state on PodInfo, snapshotted and
// restored across virtual-eviction undo, and compared by the eviction dedup. An
// empty placement means "unknown" — v1 never guesses a zone.
type NUMAPlacement []ZoneCharge

func (p NUMAPlacement) Clone() NUMAPlacement {
	if p == nil {
		return nil
	}
	out := make(NUMAPlacement, len(p))
	for i, charge := range p {
		out[i] = ZoneCharge{Zone: charge.Zone, Amount: cloneResourceList(charge.Amount)}
	}
	return out
}

// Zones returns the placement's zone ids in order — used for dedup comparison,
// where only the zone identity (not the amounts) matters.
func (p NUMAPlacement) Zones() []string {
	zones := make([]string, len(p))
	for i, charge := range p {
		zones[i] = charge.Zone
	}
	return zones
}

func cloneResourceList(list v1.ResourceList) v1.ResourceList {
	if list == nil {
		return nil
	}
	out := make(v1.ResourceList, len(list))
	for name, qty := range list {
		out[name] = qty.DeepCopy()
	}
	return out
}

// numaPlacementFromPod reads a pod's NUMA placement from its annotations,
// preferring the agent-observed placement over the scheduler-predicted one.
// Returns nil when neither annotation is present or parseable.
func numaPlacementFromPod(pod *v1.Pod) NUMAPlacement {
	for _, key := range []string{NUMAPlacementObservedAnnotation, NUMAPlacementPredictedAnnotation} {
		raw, ok := pod.Annotations[key]
		if !ok || raw == "" {
			continue
		}
		var placement NUMAPlacement
		if err := json.Unmarshal([]byte(raw), &placement); err != nil {
			log.InfraLogger.V(3).Warnf("Failed to parse NUMA placement annotation %s on pod %s/%s: %v",
				key, pod.Namespace, pod.Name, err)
			continue
		}
		if len(placement) > 0 {
			return placement
		}
	}
	return nil
}
