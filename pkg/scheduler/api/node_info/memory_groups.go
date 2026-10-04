// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	commonresources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type MemoryGroupStatus int

const (
	MemoryGroupsUnknown MemoryGroupStatus = iota
	MemoryGroupsKnown
)

type MemoryGroupState struct {
	Status MemoryGroupStatus
	Groups map[pod_info.NUMAMask]*MemoryGroup
}

type MemoryGroup struct {
	Owners map[types.UID]v1.ResourceList
}

func (state *MemoryGroupState) Clone() *MemoryGroupState {
	if state == nil {
		return nil
	}
	result := &MemoryGroupState{Status: state.Status, Groups: map[pod_info.NUMAMask]*MemoryGroup{}}
	if state.Groups == nil {
		result.Groups = nil
	}
	for mask, group := range state.Groups {
		copyGroup := &MemoryGroup{Owners: map[types.UID]v1.ResourceList{}}
		for owner, amount := range group.Owners {
			copyGroup.Owners[owner] = amount.DeepCopy()
		}
		result.Groups[mask] = copyGroup
	}
	return result
}

func (state *MemoryGroupState) Compatible(mask pod_info.NUMAMask) bool {
	if mask == 0 {
		return false
	}
	for existing := range state.Groups {
		if existing != mask && existing.Intersects(mask) {
			return false
		}
	}
	return true
}

func (topo *NumaTopology) MemoryCapacity(mask pod_info.NUMAMask, name v1.ResourceName) int64 {
	index := topo.VectorMap.GetIndex(name)
	if index < 0 {
		return -1
	}
	var capacity int64
	for _, zone := range mask.Indices() {
		if zone >= len(topo.Zones) {
			return 0
		}
		zoneCapacity := topo.Zones[zone].Allocatable.Get(index)
		if zoneCapacity < 0 || zoneCapacity >= float64(math.MaxInt64) {
			return -1
		}
		if int64(zoneCapacity) > math.MaxInt64-capacity {
			return -1
		}
		capacity += int64(zoneCapacity)
	}
	return capacity
}

func (state *MemoryGroupState) Available(topo *NumaTopology, mask pod_info.NUMAMask, name v1.ResourceName) int64 {
	capacity := topo.MemoryCapacity(mask, name)
	if capacity < 0 {
		return capacity
	}
	if group := state.Groups[mask]; group != nil {
		for _, amount := range group.Owners {
			quantity := amount[name]
			amount, valid := quantity.AsInt64()
			if !valid || amount < 0 || amount > capacity {
				return -1
			}
			capacity -= amount
		}
	}
	return capacity
}

// ValidateOwner checks replacement ownership without changing the ledger.
func (state *MemoryGroupState) ValidateOwner(topo *NumaTopology, owner types.UID, placements []pod_info.MemoryGroupPlacement) error {
	working := &MemoryGroupState{Groups: map[pod_info.NUMAMask]*MemoryGroup{}}
	for mask, group := range state.Groups {
		owners := map[types.UID]v1.ResourceList{}
		for existingOwner, amount := range group.Owners {
			if existingOwner != owner {
				owners[existingOwner] = amount
			}
		}
		if len(owners) > 0 {
			working.Groups[mask] = &MemoryGroup{Owners: owners}
		}
	}
	for _, placement := range placements {
		if !working.Compatible(placement.Mask) {
			return fmt.Errorf("incompatible memory mask %v", placement.Mask)
		}
		for _, zone := range placement.Mask.Indices() {
			if zone >= len(topo.Zones) {
				return fmt.Errorf("memory mask references unknown zone %d", zone)
			}
		}
		group := working.Groups[placement.Mask]
		if group == nil {
			group = &MemoryGroup{Owners: map[types.UID]v1.ResourceList{}}
			working.Groups[placement.Mask] = group
		}
		amount := group.Owners[owner]
		if amount == nil {
			amount = v1.ResourceList{}
		}
		for name, quantity := range placement.Amount {
			if !commonresources.IsMemoryResource(name) || !topo.Resources.Has(name) || quantity.Sign() < 0 {
				return fmt.Errorf("invalid managed memory amount for %s", name)
			}
			bytes, valid := quantity.AsInt64()
			if !valid {
				return fmt.Errorf("invalid integer memory amount for %s", name)
			}
			if bytes > working.Available(topo, placement.Mask, name) {
				return fmt.Errorf("insufficient %s in memory mask %v", name, placement.Mask)
			}
			previous := amount[name]
			previous.Add(quantity)
			if _, valid := previous.AsInt64(); !valid {
				return fmt.Errorf("memory amount overflow for %s", name)
			}
			amount[name] = previous
		}
		group.Owners[owner] = amount
	}
	return nil
}

// ApplyOwner replaces validated ownership.
func (state *MemoryGroupState) ApplyOwner(owner types.UID, placements []pod_info.MemoryGroupPlacement) {
	state.RemoveOwner(owner)
	if state.Groups == nil {
		state.Groups = map[pod_info.NUMAMask]*MemoryGroup{}
	}
	for _, placement := range placements {
		group := state.Groups[placement.Mask]
		if group == nil {
			group = &MemoryGroup{Owners: map[types.UID]v1.ResourceList{}}
			state.Groups[placement.Mask] = group
		}
		amount := group.Owners[owner]
		if amount == nil {
			amount = v1.ResourceList{}
		}
		for name, quantity := range placement.Amount {
			previous := amount[name]
			previous.Add(quantity.DeepCopy())
			amount[name] = previous
		}
		group.Owners[owner] = amount
	}
}

func (state *MemoryGroupState) RemoveOwner(owner types.UID) {
	if state == nil {
		return
	}
	for mask, group := range state.Groups {
		delete(group.Owners, owner)
		if len(group.Owners) == 0 {
			delete(state.Groups, mask)
		}
	}
}
