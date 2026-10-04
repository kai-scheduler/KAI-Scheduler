// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"fmt"
	"math/bits"
	"sort"

	v1 "k8s.io/api/core/v1"
)

// ZonePlacement is a task's placement on one NUMA zone: the zone's index (into the
// numa plugin's per-cycle nodeTopology.zones) and the exact per-resource amount
// placed there. The index is the internal scheduler representation; translation
// to/from the durable zone id happens only at the persistence boundary (BindRequest
// field and pod annotation), which the numa plugin owns.
type ZonePlacement struct {
	ZoneIndex int
	Amount    v1.ResourceList
}

// MaxNUMAZones bounds exponential hint enumeration to kubelet's default topology limit.
const MaxNUMAZones = 8

type NUMAMask uint64

func NewNUMAMask(indices []int) (NUMAMask, error) {
	var mask NUMAMask
	for _, index := range indices {
		if index < 0 || index >= 64 {
			return 0, fmt.Errorf("NUMA index %d outside supported range [0, 64)", index)
		}
		mask |= 1 << index
	}
	return mask, nil
}

func (mask NUMAMask) Indices() []int {
	indices := make([]int, 0, mask.Width())
	for index := 0; index < 64; index++ {
		if mask&(1<<index) != 0 {
			indices = append(indices, index)
		}
	}
	return indices
}

func (mask NUMAMask) Width() int { return bits.OnesCount64(uint64(mask)) }

func (mask NUMAMask) Intersects(other NUMAMask) bool { return mask&other != 0 }

type MemoryGroupPlacement struct {
	Mask   NUMAMask
	Amount v1.ResourceList
}

// NUMAPlacement carries per-zone charges and Memory Manager ownership separately.
type NUMAPlacement struct {
	Zones        []ZonePlacement
	MemoryGroups []MemoryGroupPlacement
}

func (p NUMAPlacement) IsEmpty() bool {
	return len(p.Zones) == 0 && len(p.MemoryGroups) == 0
}

func (p NUMAPlacement) Clone() NUMAPlacement {
	out := NUMAPlacement{}
	if p.Zones != nil {
		out.Zones = make([]ZonePlacement, len(p.Zones))
	}
	for index, charge := range p.Zones {
		out.Zones[index] = ZonePlacement{ZoneIndex: charge.ZoneIndex, Amount: cloneResourceList(charge.Amount)}
	}
	if p.MemoryGroups != nil {
		out.MemoryGroups = make([]MemoryGroupPlacement, len(p.MemoryGroups))
	}
	for index, group := range p.MemoryGroups {
		out.MemoryGroups[index] = MemoryGroupPlacement{Mask: group.Mask, Amount: cloneResourceList(group.Amount)}
	}
	return out
}

// ZoneIndices includes memory-only zones for placement scoring.
func (p NUMAPlacement) ZoneIndices() []int {
	zoneSet := make(map[int]struct{})
	for _, charge := range p.Zones {
		zoneSet[charge.ZoneIndex] = struct{}{}
	}
	for _, group := range p.MemoryGroups {
		for _, index := range group.Mask.Indices() {
			zoneSet[index] = struct{}{}
		}
	}
	indices := make([]int, 0, len(zoneSet))
	for index := range zoneSet {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	return indices
}

func (p NUMAPlacement) Equal(other NUMAPlacement) bool {
	if len(p.Zones) != len(other.Zones) || len(p.MemoryGroups) != len(other.MemoryGroups) {
		return false
	}
	for index := range p.Zones {
		if p.Zones[index].ZoneIndex != other.Zones[index].ZoneIndex || !equal(p.Zones[index].Amount, other.Zones[index].Amount) {
			return false
		}
	}
	for index := range p.MemoryGroups {
		if p.MemoryGroups[index].Mask != other.MemoryGroups[index].Mask || !equal(p.MemoryGroups[index].Amount, other.MemoryGroups[index].Amount) {
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
