// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

var errMemoryGroupConflict = errors.New("NUMA memory placement conflict")
var errMemoryGroupCapacity = errors.New("Insufficient NUMA memory capacity")
var errMemoryStateUnknown = errors.New("node memory group state is unknown")

type memorySolver struct {
	taskUID     types.UID
	topology    *node_info.NumaTopology
	resources   sets.Set[v1.ResourceName]
	groups      *node_info.MemoryGroupState
	allocations []pod_info.MemoryGroupPlacement
	hints       *memoryHintSet
}

type memoryHintSet struct {
	request  v1.ResourceList
	hints    []topologymanager.TopologyHint
	physical []pod_info.NUMAMask
}

func newMemorySolver(task *pod_info.PodInfo, topology *node_info.NumaTopology, resources sets.Set[v1.ResourceName]) *memorySolver {
	return &memorySolver{taskUID: types.UID(task.UID), topology: topology, resources: resources, groups: topology.MemoryGroups.Clone()}
}

func (solver *memorySolver) allocateUnit(sequence int, unit admissionUnit, hint topologymanager.TopologyHint) error {
	if unit.ordinaryInit {
		return nil
	}
	request := managedMemoryRequest(unit.container.Resources.Requests, solver.resources)
	if len(request) == 0 {
		return nil
	}
	mask, err := solver.finalMemoryMask(hint, request)
	if err != nil {
		return err
	}
	owner := types.UID(fmt.Sprintf("%s/container/%d", solver.taskUID, sequence))
	return solver.allocateMemory(owner, mask, request)
}

func (solver *memorySolver) persistentGroups() []pod_info.MemoryGroupPlacement {
	groups := map[pod_info.NUMAMask]v1.ResourceList{}
	for _, placement := range solver.allocations {
		amount := groups[placement.Mask]
		if amount == nil {
			amount = v1.ResourceList{}
		}
		for name, quantity := range placement.Amount {
			previous := amount[name]
			previous.Add(quantity)
			amount[name] = previous
		}
		groups[placement.Mask] = amount
	}
	var result []pod_info.MemoryGroupPlacement
	for mask, amount := range groups {
		result = append(result, pod_info.MemoryGroupPlacement{Mask: mask, Amount: amount})
	}
	sort.Slice(result, func(first, second int) bool {
		return result[first].Mask < result[second].Mask
	})
	return result
}

func managedMemoryRequest(request v1.ResourceList, managed sets.Set[v1.ResourceName]) v1.ResourceList {
	result := v1.ResourceList{}
	for name, quantity := range request {
		if managed.Has(name) && quantity.Sign() > 0 {
			result[name] = quantity.DeepCopy()
		}
	}
	return result
}

func (solver *memorySolver) fitsPhysically(mask pod_info.NUMAMask, request v1.ResourceList) bool {
	return fitsMemoryAmounts(request, func(name v1.ResourceName) int64 {
		return solver.topology.MemoryCapacity(mask, name)
	})
}

func (solver *memorySolver) fitsGroups(mask pod_info.NUMAMask, request v1.ResourceList) bool {
	if !solver.groups.Compatible(mask) {
		return false
	}
	return fitsMemoryAmounts(request, func(name v1.ResourceName) int64 {
		return solver.groups.Available(solver.topology, mask, name)
	})
}

func fitsMemoryAmounts(request v1.ResourceList, availableFor func(v1.ResourceName) int64) bool {
	for name, quantity := range request {
		requested, valid := quantity.AsInt64()
		if !valid || requested < 0 {
			return false
		}
		available := availableFor(name)
		if available < 0 {
			return false
		}
		if available < requested {
			return false
		}
	}
	return true
}

func (solver *memorySolver) hintsFor(request v1.ResourceList) *memoryHintSet {
	if solver.hints != nil && equalMemoryRequests(solver.hints.request, request) {
		return solver.hints
	}
	result := &memoryHintSet{request: request.DeepCopy()}
	preferredWidth := len(solver.topology.Zones) + 1
	enumerateNUMAMasks(solver.topology, func(mask pod_info.NUMAMask) {
		if !solver.fitsPhysically(mask, request) {
			return
		}
		result.physical = append(result.physical, mask)
		if mask.Width() < preferredWidth {
			preferredWidth = mask.Width()
		}
		if !solver.fitsGroups(mask, request) {
			return
		}
		affinity := topologyAffinity(mask)
		result.hints = append(result.hints, topologymanager.TopologyHint{NUMANodeAffinity: affinity})
	})
	for index := range result.hints {
		result.hints[index].Preferred = result.hints[index].NUMANodeAffinity.Count() == preferredWidth
	}
	solver.hints = result
	return result
}

func equalMemoryRequests(first, second v1.ResourceList) bool {
	if len(first) != len(second) {
		return false
	}
	for name, quantity := range first {
		other, exists := second[name]
		if !exists || quantity.Cmp(other) != 0 {
			return false
		}
	}
	return true
}

func (solver *memorySolver) finalMemoryMask(merged topologymanager.TopologyHint, request v1.ResourceList) (pod_info.NUMAMask, error) {
	if merged.NUMANodeAffinity != nil {
		mask := numaMask(merged.NUMANodeAffinity)
		if solver.fitsGroups(mask, request) {
			return mask, nil
		}
	}
	var best *topologymanager.TopologyHint
	for _, hint := range solver.hintsFor(request).hints {
		if merged.NUMANodeAffinity != nil && !bitmask.And(merged.NUMANodeAffinity, hint.NUMANodeAffinity).IsEqual(merged.NUMANodeAffinity) {
			continue
		}
		if best == nil || hint.Preferred && !best.Preferred || hint.Preferred == best.Preferred && hint.NUMANodeAffinity.IsNarrowerThan(best.NUMANodeAffinity) {
			copyHint := hint
			best = &copyHint
		}
	}
	if best == nil {
		return 0, solver.noMemoryHintError(request, merged.NUMANodeAffinity)
	}
	if merged.Preferred && !best.Preferred {
		return 0, fmt.Errorf("%w: extending memory affinity requires a non-preferred mask", errNotNumaAligned)
	}
	mask := numaMask(best.NUMANodeAffinity)
	return mask, nil
}

func (solver *memorySolver) noMemoryHintError(request v1.ResourceList, affinity bitmask.BitMask) error {
	compatible := false
	physical := false
	for _, mask := range solver.hintsFor(request).physical {
		if affinity != nil {
			candidate := topologyAffinity(mask)
			if !bitmask.And(candidate, affinity).IsEqual(affinity) {
				continue
			}
		}
		physical = true
		if solver.groups.Compatible(mask) {
			compatible = true
		}
	}
	if physical && !compatible {
		masks := make([]pod_info.NUMAMask, 0, len(solver.groups.Groups))
		for mask := range solver.groups.Groups {
			masks = append(masks, mask)
		}
		sort.Slice(masks, func(first, second int) bool { return masks[first] < masks[second] })
		indices := make([][]int, 0, len(masks))
		for _, mask := range masks {
			indices = append(indices, mask.Indices())
		}
		return fmt.Errorf("%w: requested %s cannot fit without conflicting with existing memory groups on NUMA nodes %v%s",
			errMemoryGroupConflict, formatMemoryRequest(request), indices, memoryAffinityConstraint(affinity))
	}
	return fmt.Errorf("%w for requested %s%s", errMemoryGroupCapacity,
		formatMemoryRequest(request), memoryAffinityConstraint(affinity))
}

func formatMemoryRequest(request v1.ResourceList) string {
	names := make([]string, 0, len(request))
	for name := range request {
		names = append(names, string(name))
	}
	sort.Strings(names)
	resources := make([]string, 0, len(names))
	for _, name := range names {
		quantity := request[v1.ResourceName(name)]
		resources = append(resources, name+": "+quantity.String())
	}
	return "{" + strings.Join(resources, ", ") + "}"
}

func memoryAffinityConstraint(affinity bitmask.BitMask) string {
	if affinity == nil {
		return ""
	}
	return fmt.Sprintf(". Memory placement must include NUMA nodes %v", affinity.GetBits())
}

func (solver *memorySolver) allocateMemory(owner types.UID, mask pod_info.NUMAMask, request v1.ResourceList) error {
	solver.hints = nil
	placement := pod_info.MemoryGroupPlacement{Mask: mask, Amount: request.DeepCopy()}
	if err := solver.groups.ValidateOwner(solver.topology, owner, []pod_info.MemoryGroupPlacement{placement}); err != nil {
		return err
	}
	solver.groups.ApplyOwner(owner, []pod_info.MemoryGroupPlacement{placement})
	solver.allocations = append(solver.allocations, placement)
	return nil
}

func (solver *memorySolver) providerHints(request resource_info.ResourceVector, preferredOnly bool) ([]topologymanager.TopologyHint, error) {
	memoryRequest := v1.ResourceList{}
	for name := range solver.resources {
		index := solver.topology.VectorMap.GetIndex(name)
		if need := request.Get(index); need > 0 {
			memoryRequest[name] = *resource.NewQuantity(int64(need), resource.DecimalSI)
		}
	}
	if len(memoryRequest) == 0 {
		return nil, nil
	}
	hints := solver.hintsFor(memoryRequest).hints
	if len(hints) == 0 {
		return nil, solver.noMemoryHintError(memoryRequest, nil)
	}
	if !preferredOnly {
		return hints, nil
	}
	preferred := make([]topologymanager.TopologyHint, 0, len(hints))
	for _, hint := range hints {
		if hint.Preferred && (solver.topology.Policy != node_info.TopologyPolicySingleNUMANode || hint.NUMANodeAffinity.Count() == 1) {
			preferred = append(preferred, hint)
		}
	}
	if len(preferred) == 0 {
		return []topologymanager.TopologyHint{{Preferred: false}}, nil
	}
	return preferred, nil
}
