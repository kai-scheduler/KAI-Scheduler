// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	v1 "k8s.io/api/core/v1"
)

// ZonePlacement is a task's placement on one NUMA zone: the zone's index (into the
// numa plugin's per-cycle nodeTopology.zones) and the exact per-resource amount
// placed there. The index is the internal scheduler representation; translation
// to/from the durable zone id happens only at the persistence boundary (BindRequest
// field and pod annotation).
type ZonePlacement struct {
	ZoneIndex int
	Amount    v1.ResourceList
}

// NUMAPlacement is a task's NUMA placement — its zone(s) and per-zone amounts. Could be empty if the placement is unknown.
type NUMAPlacement []ZonePlacement

func (p NUMAPlacement) Clone() NUMAPlacement {
	if p == nil {
		return nil
	}
	out := make(NUMAPlacement, len(p))
	for i, charge := range p {
		out[i] = ZonePlacement{ZoneIndex: charge.ZoneIndex, Amount: cloneResourceList(charge.Amount)}
	}
	return out
}

// ZoneIndices returns the placement's zone indices in order.
func (p NUMAPlacement) ZoneIndices() []int {
	indices := make([]int, len(p))
	for i, charge := range p {
		indices[i] = charge.ZoneIndex
	}
	return indices
}

func (p NUMAPlacement) Equal(other NUMAPlacement) bool {
	if len(p) != len(other) {
		return false
	}
	for i := range p {
		if p[i].ZoneIndex != other[i].ZoneIndex || !equal(p[i].Amount, other[i].Amount) {
			return false
		}
	}
	return true
}

func equal(a, b v1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for name, qa := range a {
		qb, ok := b[name]
		if !ok || qa.Cmp(qb) != 0 {
			return false
		}
	}
	return true
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
