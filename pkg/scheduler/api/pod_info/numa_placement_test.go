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
	orig := NUMAPlacement{{ZoneIndex: 0, Amount: v1.ResourceList{"cpu": resource.MustParse("4")}}}
	clone := orig.Clone()

	// Mutating the clone must not affect the original (deep copy).
	clone[0].Amount["cpu"] = resource.MustParse("8")
	clone[0].ZoneIndex = 1

	origCPU := orig[0].Amount["cpu"]
	assert.Equal(t, int64(4), origCPU.Value(), "original amount unchanged")
	assert.Equal(t, 0, orig[0].ZoneIndex, "original zone index unchanged")

	assert.Nil(t, NUMAPlacement(nil).Clone())
}

func TestNumaPlacementZones(t *testing.T) {
	p := NUMAPlacement{{ZoneIndex: 0}, {ZoneIndex: 1}}
	assert.Equal(t, []int{0, 1}, p.ZoneIndices())
}

func TestNumaPlacementEqual(t *testing.T) {
	amt := func(cpu string) v1.ResourceList { return v1.ResourceList{"cpu": resource.MustParse(cpu)} }
	p := NUMAPlacement{{ZoneIndex: 0, Amount: amt("2")}, {ZoneIndex: 1, Amount: amt("4")}}

	assert.True(t, p.Equal(NUMAPlacement{{ZoneIndex: 0, Amount: amt("2")}, {ZoneIndex: 1, Amount: amt("4")}}))
	assert.False(t, p.Equal(NUMAPlacement{{ZoneIndex: 0, Amount: amt("2")}}), "different length")
	assert.False(t, p.Equal(NUMAPlacement{{ZoneIndex: 1, Amount: amt("4")}, {ZoneIndex: 0, Amount: amt("2")}}), "order matters")
	assert.False(t, p.Equal(NUMAPlacement{{ZoneIndex: 0, Amount: amt("4")}, {ZoneIndex: 1, Amount: amt("2")}}),
		"same zones, different per-zone split is NOT equal")
}
