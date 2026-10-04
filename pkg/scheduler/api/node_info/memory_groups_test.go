// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func memoryLedgerTopology() *NumaTopology {
	amount := v1.ResourceList{v1.ResourceMemory: resource.MustParse("100Gi")}
	return newNumaTopology(TopologyPolicyRestricted, TopologyScopeContainer, resource_info.NewResourceVectorMap(), []NumaZoneSpec{
		{ID: "node-0", Allocatable: amount, Available: amount},
		{ID: "node-1", Allocatable: amount, Available: amount},
	})
}

func TestMemoryGroupOwnerLifecycle(t *testing.T) {
	topology := memoryLedgerTopology()
	mask, err := pod_info.NewNUMAMask([]int{0, 1})
	require.NoError(t, err)
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	allocation := []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("120Gi")}}}
	require.NoError(t, state.ValidateOwner(topology, "first", allocation))
	state.ApplyOwner("first", allocation)
	require.NoError(t, state.ValidateOwner(topology, "first", allocation), "idempotent ownership replacement")
	second := []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("30Gi")}}}
	require.NoError(t, state.ValidateOwner(topology, "second", second))
	state.ApplyOwner("second", second)
	free := resource.MustParse("50Gi")
	require.Equal(t, free.Value(), state.Available(topology, mask, v1.ResourceMemory))
	clone := state.Clone()
	clone.RemoveOwner("first")
	require.Len(t, clone.Groups, 1)
	require.Len(t, state.Groups[mask].Owners, 2)
	clone.RemoveOwner("second")
	require.Empty(t, clone.Groups)
}

func TestMemoryGroupValidationAtomicity(t *testing.T) {
	topology := memoryLedgerTopology()
	mask, _ := pod_info.NewNUMAMask([]int{0, 1})
	singleton, _ := pod_info.NewNUMAMask([]int{0})
	unknown, _ := pod_info.NewNUMAMask([]int{2})
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	allocation := []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("120Gi")}}}
	require.NoError(t, state.ValidateOwner(topology, "first", allocation))
	state.ApplyOwner("first", allocation)
	for _, invalid := range []pod_info.MemoryGroupPlacement{
		{Mask: singleton, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("1Gi")}},
		{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("90Gi")}},
		{Mask: unknown, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("0")}},
		{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("-1")}},
		{Mask: mask, Amount: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}},
		{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("9223372036854775808")}},
	} {
		before := state.Clone()
		require.Error(t, state.ValidateOwner(topology, "invalid", []pod_info.MemoryGroupPlacement{invalid}))
		require.Equal(t, before, state)
	}
}

func TestMemoryGroupDuplicateMasksCannotExceedCapacity(t *testing.T) {
	topology := memoryLedgerTopology()
	mask, _ := pod_info.NewNUMAMask([]int{0, 1})
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	placement := pod_info.MemoryGroupPlacement{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("120Gi")}}
	require.Error(t, state.ValidateOwner(topology, "duplicate", []pod_info.MemoryGroupPlacement{placement, placement}))
	require.Empty(t, state.Groups)
}

func TestMemoryGroupZeroOwnerPinsUntilRemoval(t *testing.T) {
	topology := memoryLedgerTopology()
	mask, _ := pod_info.NewNUMAMask([]int{0, 1})
	singleton, _ := pod_info.NewNUMAMask([]int{0})
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	allocation := []pod_info.MemoryGroupPlacement{{Mask: mask, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("0")}}}
	require.NoError(t, state.ValidateOwner(topology, "zero", allocation))
	state.ApplyOwner("zero", allocation)
	require.False(t, state.Compatible(singleton))
	state.RemoveOwner("zero")
	require.True(t, state.Compatible(singleton))
}

func TestMemoryOwnerValidationDoesNotMutateState(t *testing.T) {
	topology := memoryLedgerTopology()
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	allocation := []pod_info.MemoryGroupPlacement{{Mask: 3, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("120Gi")}}}
	before := state.Clone()
	require.NoError(t, state.ValidateOwner(topology, "owner", allocation))
	require.Equal(t, before, state)
	state.ApplyOwner("owner", allocation)
	before = state.Clone()
	require.NoError(t, state.ValidateOwner(topology, "owner", allocation))
	require.Equal(t, before, state)
	allocation[0].Amount[v1.ResourceMemory] = resource.MustParse("1Gi")
	require.Equal(t, int64(80<<30), state.Available(topology, 3, v1.ResourceMemory))
	require.Equal(t, int64(200<<30), topology.MemoryCapacity(3, v1.ResourceMemory))
}

func TestMemoryLedgerRetainsBytePrecisionAtFloatBoundary(t *testing.T) {
	capacity := v1.ResourceList{v1.ResourceMemory: resource.MustParse("9007199254740992")}
	topology := newNumaTopology(TopologyPolicyRestricted, TopologyScopeContainer, resource_info.NewResourceVectorMap(), []NumaZoneSpec{
		{ID: "node-0", Allocatable: capacity, Available: capacity},
	})
	state := &MemoryGroupState{Status: MemoryGroupsKnown}
	allocation := []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("1")}}}
	require.NoError(t, state.ValidateOwner(topology, "first", allocation))
	state.ApplyOwner("first", allocation)
	require.Equal(t, int64(9007199254740991), state.Available(topology, 1, v1.ResourceMemory))
	require.Error(t, state.ValidateOwner(topology, "second", []pod_info.MemoryGroupPlacement{{Mask: 1, Amount: capacity}}))
}
