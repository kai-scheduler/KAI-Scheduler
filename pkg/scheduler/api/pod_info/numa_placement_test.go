// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNumaPlacementClone(t *testing.T) {
	orig := NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0, Amount: v1.ResourceList{"cpu": resource.MustParse("4")}}}, MemoryGroups: []MemoryGroupPlacement{{Mask: 3, Amount: v1.ResourceList{"memory": resource.MustParse("8Gi")}}}}
	clone := orig.Clone()

	// Mutating the clone must not affect the original (deep copy).
	clone.Zones[0].Amount["cpu"] = resource.MustParse("8")
	clone.Zones[0].ZoneIndex = 1
	clone.MemoryGroups[0].Amount["memory"] = resource.MustParse("16Gi")

	origCPU := orig.Zones[0].Amount["cpu"]
	assert.Equal(t, int64(4), origCPU.Value(), "original amount unchanged")
	assert.Equal(t, 0, orig.Zones[0].ZoneIndex, "original zone index unchanged")
	assert.Equal(t, resource.MustParse("8Gi"), orig.MemoryGroups[0].Amount["memory"])

	assert.True(t, (NUMAPlacement{}).Clone().IsEmpty())
}

func TestNumaPlacementZones(t *testing.T) {
	p := NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0}, {ZoneIndex: 1}}}
	assert.Equal(t, []int{0, 1}, p.ZoneIndices())
}

func TestNumaPlacementEqual(t *testing.T) {
	amt := func(cpu string) v1.ResourceList { return v1.ResourceList{"cpu": resource.MustParse(cpu)} }
	p := NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0, Amount: amt("2")}, {ZoneIndex: 1, Amount: amt("4")}}}

	assert.True(t, p.Equal(NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0, Amount: amt("2")}, {ZoneIndex: 1, Amount: amt("4")}}}))
	assert.False(t, p.Equal(NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0, Amount: amt("2")}}}), "different length")
	assert.False(t, p.Equal(NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 1, Amount: amt("4")}, {ZoneIndex: 0, Amount: amt("2")}}}), "order matters")
	assert.False(t, p.Equal(NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 0, Amount: amt("4")}, {ZoneIndex: 1, Amount: amt("2")}}}),
		"same zones, different per-zone split is NOT equal")
}

func TestNUMAMask(t *testing.T) {
	mask, err := NewNUMAMask([]int{63, 1, 0, 1})
	assert.NoError(t, err)
	assert.Equal(t, []int{0, 1, 63}, mask.Indices())
	assert.Equal(t, 3, mask.Width())
	assert.True(t, mask.Intersects(2))
	assert.False(t, mask.Intersects(4))
	for _, index := range []int{-1, 64} {
		_, err := NewNUMAMask([]int{index})
		assert.Error(t, err)
	}
}

func TestMemoryGroupsPlacement(t *testing.T) {
	placement := NUMAPlacement{Zones: []ZonePlacement{{ZoneIndex: 2}}, MemoryGroups: []MemoryGroupPlacement{{Mask: 3, Amount: v1.ResourceList{"memory": resource.MustParse("1Gi")}}}}
	assert.Equal(t, []int{0, 1, 2}, placement.ZoneIndices())
	assert.False(t, placement.IsEmpty())
	cloned := placement.Clone()
	cloned.MemoryGroups[0].Amount["memory"] = resource.MustParse("1024Mi")
	assert.True(t, placement.Equal(cloned))
	cloned.MemoryGroups[0].Mask = 1
	assert.False(t, placement.Equal(cloned))
}
