// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

func TestNumaPlacementToZones(t *testing.T) {
	node := &node_info.NodeInfo{
		Name: "node-a",
		NumaTopology: &node_info.NumaTopology{
			Zones: []*node_info.NumaZone{{ID: "node-0"}, {ID: "node-1"}},
		},
	}
	pod := &pod_info.PodInfo{
		NUMAPlacement: pod_info.NUMAPlacement{
			Zones: []pod_info.ZonePlacement{{ZoneIndex: 1, Amount: v1.ResourceList{"cpu": resource.MustParse("3")}}},
		},
	}

	t.Run("translates index to durable zone id", func(t *testing.T) {
		zones := numaPlacementToZones(pod, node)
		assert.Len(t, zones, 1)
		assert.Equal(t, "node-1", zones[0].Zone)
		cpu := zones[0].Amount["cpu"]
		assert.Equal(t, int64(3), cpu.Value())
	})

	t.Run("nil when task has no placement", func(t *testing.T) {
		assert.Nil(t, numaPlacementToZones(&pod_info.PodInfo{}, node))
	})

	t.Run("nil when node has no topology", func(t *testing.T) {
		assert.Nil(t, numaPlacementToZones(pod, &node_info.NodeInfo{Name: "bare"}))
	})

	t.Run("out-of-range index is skipped", func(t *testing.T) {
		bad := &pod_info.PodInfo{NUMAPlacement: pod_info.NUMAPlacement{Zones: []pod_info.ZonePlacement{{ZoneIndex: 5}}}}
		assert.Empty(t, numaPlacementToZones(bad, node))
	})
}

func TestNumaPlacementToMemoryGroups(t *testing.T) {
	node := &node_info.NodeInfo{NumaTopology: &node_info.NumaTopology{Zones: []*node_info.NumaZone{{ID: "zone-a"}, {ID: "zone-b"}}}}
	pod := &pod_info.PodInfo{NUMAPlacement: pod_info.NUMAPlacement{MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 3, Amount: v1.ResourceList{v1.ResourceMemory: resource.MustParse("8Gi")}}}}}
	groups := numaPlacementToMemoryGroups(pod, node)
	assert.Len(t, groups, 1)
	assert.Equal(t, []string{"zone-a", "zone-b"}, groups[0].MemoryNodes)
	groups[0].Amount[v1.ResourceMemory] = resource.MustParse("16Gi")
	assert.Equal(t, resource.MustParse("8Gi"), pod.NUMAPlacement.MemoryGroups[0].Amount[v1.ResourceMemory])
	pod.NUMAPlacement.MemoryGroups[0].Mask = 4
	assert.Nil(t, numaPlacementToMemoryGroups(pod, node))
}

func TestNumaPlacementConversionMissingInputs(t *testing.T) {
	node := &node_info.NodeInfo{NumaTopology: &node_info.NumaTopology{Zones: []*node_info.NumaZone{{ID: "node-0"}}}}
	pod := &pod_info.PodInfo{NUMAPlacement: pod_info.NUMAPlacement{
		Zones:        []pod_info.ZonePlacement{{ZoneIndex: 0}},
		MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 1}},
	}}
	tests := []struct {
		name string
		pod  *pod_info.PodInfo
		node *node_info.NodeInfo
	}{
		{name: "nil pod", node: node},
		{name: "nil node", pod: pod},
		{name: "no topology", pod: pod, node: &node_info.NodeInfo{}},
		{name: "empty placement", pod: &pod_info.PodInfo{}, node: node},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Nil(t, numaPlacementToZones(test.pod, test.node))
			assert.Nil(t, numaPlacementToMemoryGroups(test.pod, test.node))
		})
	}
}
