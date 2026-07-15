// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

// zoneAmount reads a zone vector's amount for a resource in its natural unit (cpu in cores, others
// by count), translating from the vector's milli-cpu storage.
func zoneAmount(vec resource_info.ResourceVector, vm *resource_info.ResourceVectorMap, name v1.ResourceName) int64 {
	idx := vm.GetIndex(name)
	val := vec.Get(idx)
	if idx == resource_info.CPUIndex {
		return int64(val) / 1000
	}
	return int64(val)
}

func numaNodeZone(name string, available map[string]string) nrtv1alpha2.Zone {
	return numaNodeZoneWithAllocatable(name, available, available)
}

func numaNodeZoneWithAllocatable(name string, available, allocatable map[string]string) nrtv1alpha2.Zone {
	var resources nrtv1alpha2.ResourceInfoList
	for resName, qty := range available {
		resources = append(resources, nrtv1alpha2.ResourceInfo{
			Name:        resName,
			Available:   resource.MustParse(qty),
			Allocatable: resource.MustParse(allocatable[resName]),
		})
	}
	return nrtv1alpha2.Zone{Name: name, Type: zoneTypeNode, Resources: resources}
}

func nrtWithAttributes(policy, scope string, zones ...nrtv1alpha2.Zone) *nrtv1alpha2.NodeResourceTopology {
	var attrs nrtv1alpha2.AttributeList
	if policy != "" {
		attrs = append(attrs, nrtv1alpha2.AttributeInfo{Name: attrTopologyManagerPolicy, Value: policy})
	}
	if scope != "" {
		attrs = append(attrs, nrtv1alpha2.AttributeInfo{Name: attrTopologyManagerScope, Value: scope})
	}
	return &nrtv1alpha2.NodeResourceTopology{Zones: zones, Attributes: attrs}
}

func TestParsePolicyAndScope(t *testing.T) {
	zone := numaNodeZone("node-0", map[string]string{"cpu": "4"})

	tests := map[string]struct {
		nrt           *nrtv1alpha2.NodeResourceTopology
		expectedPolic TopologyManagerPolicy
		expectedScope TopologyManagerScope
	}{
		"attribute single-numa-node container": {
			nrt:           nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer, zone),
			expectedPolic: TopologyPolicySingleNUMANode,
			expectedScope: TopologyScopeContainer,
		},
		"attribute single-numa-node pod": {
			nrt:           nrtWithAttributes(policyValueSingleNUMANode, scopeValuePod, zone),
			expectedPolic: TopologyPolicySingleNUMANode,
			expectedScope: TopologyScopePod,
		},
		"attribute restricted, scope defaults to container when missing": {
			nrt:           nrtWithAttributes(policyValueRestricted, "", zone),
			expectedPolic: TopologyPolicyRestricted,
			expectedScope: TopologyScopeContainer,
		},
		"attribute best-effort": {
			nrt:           nrtWithAttributes(policyValueBestEffort, scopeValuePod, zone),
			expectedPolic: TopologyPolicyBestEffort,
			expectedScope: TopologyScopePod,
		},
		"attribute none": {
			nrt:           nrtWithAttributes(policyValueNone, scopeValueContainer, zone),
			expectedPolic: TopologyPolicyNone,
			expectedScope: TopologyScopeContainer,
		},
		"no attributes, no legacy policies -> none/container": {
			nrt:           nrtWithAttributes("", "", zone),
			expectedPolic: TopologyPolicyNone,
			expectedScope: TopologyScopeContainer,
		},
		"legacy SingleNUMANodePodLevel": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodePodLevel)},
			},
			expectedPolic: TopologyPolicySingleNUMANode,
			expectedScope: TopologyScopePod,
		},
		"legacy SingleNUMANodeContainerLevel": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodeContainerLevel)},
			},
			expectedPolic: TopologyPolicySingleNUMANode,
			expectedScope: TopologyScopeContainer,
		},
		"legacy Restricted": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.Restricted)},
			},
			expectedPolic: TopologyPolicyRestricted,
			expectedScope: TopologyScopeContainer,
		},
		"attributes take precedence over legacy": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				Attributes:       nrtv1alpha2.AttributeList{{Name: attrTopologyManagerPolicy, Value: policyValueRestricted}},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodePodLevel)},
			},
			expectedPolic: TopologyPolicyRestricted,
			expectedScope: TopologyScopeContainer,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			policy, scope := parsePolicyAndScope(test.nrt)
			assert.Equal(t, test.expectedPolic, policy, "policy")
			assert.Equal(t, test.expectedScope, scope, "scope")
		})
	}
}

func TestBuildNumaTopology(t *testing.T) {
	t.Run("nil NRT returns nil", func(t *testing.T) {
		assert.Nil(t, BuildNumaTopology(nil, resource_info.NewResourceVectorMap()))
	})

	t.Run("no NUMA-node zones returns nil", func(t *testing.T) {
		nrt := &nrtv1alpha2.NodeResourceTopology{
			Zones: nrtv1alpha2.ZoneList{{Name: "socket-0", Type: "Socket"}},
		}
		assert.Nil(t, BuildNumaTopology(nrt, resource_info.NewResourceVectorMap()))
	})

	t.Run("zones, availability and reported resources are populated", func(t *testing.T) {
		nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
			numaNodeZone("node-0", map[string]string{"cpu": "4", "nvidia.com/gpu": "2"}),
			numaNodeZone("node-1", map[string]string{"cpu": "8", "memory": "16Gi"}),
			nrtv1alpha2.Zone{Name: "socket-0", Type: "Socket"}, // ignored
		)

		nt := BuildNumaTopology(nrt, resource_info.NewResourceVectorMap())

		assert.NotNil(t, nt)
		assert.Equal(t, TopologyPolicySingleNUMANode, nt.Policy)
		assert.Len(t, nt.Zones, 2, "only NUMA-node zones are kept")

		assert.Equal(t, int64(2), zoneAmount(nt.Zones[0].Available, nt.VectorMap, "nvidia.com/gpu"))
		assert.Equal(t, int64(2), zoneAmount(nt.Zones[0].Allocatable, nt.VectorMap, "nvidia.com/gpu"), "allocatable is populated")

		assert.True(t, nt.Resources.HasAll("cpu", "memory", "nvidia.com/gpu"))
		assert.Equal(t, 3, nt.Resources.Len())
	})

	// Restricted mode admits against a zone's total capacity, not its free capacity, so Allocatable
	// must be carried through independently of Available.
	t.Run("allocatable is populated independently of available", func(t *testing.T) {
		nrt := nrtWithAttributes(policyValueRestricted, scopeValueContainer,
			numaNodeZoneWithAllocatable("node-0",
				map[string]string{"cpu": "4"}, // available: free capacity
				map[string]string{"cpu": "8"}, // allocatable: total capacity
			),
		)

		nt := BuildNumaTopology(nrt, resource_info.NewResourceVectorMap())

		assert.Equal(t, int64(4), zoneAmount(nt.Zones[0].Available, nt.VectorMap, "cpu"), "available reflects free capacity")
		assert.Equal(t, int64(8), zoneAmount(nt.Zones[0].Allocatable, nt.VectorMap, "cpu"), "allocatable reflects total capacity")
	})
}

func TestBuildNumaTopologyOrdersZones(t *testing.T) {
	// Deliberately out of order, and with a two-digit id to catch lexicographic ordering bugs
	// (node-10 must come after node-2).
	nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
		numaNodeZone("node-10", map[string]string{"cpu": "1"}),
		numaNodeZone("node-2", map[string]string{"cpu": "1"}),
		numaNodeZone("node-0", map[string]string{"cpu": "1"}),
	)

	nt := BuildNumaTopology(nrt, resource_info.NewResourceVectorMap())

	ids := []string{nt.Zones[0].ID, nt.Zones[1].ID, nt.Zones[2].ID}
	assert.Equal(t, []string{"node-0", "node-2", "node-10"}, ids)
}

func TestNumaTopologyClone(t *testing.T) {
	nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
		numaNodeZone("node-0", map[string]string{"cpu": "4"}),
	)
	orig := BuildNumaTopology(nrt, resource_info.NewResourceVectorMap())
	clone := orig.Clone()

	// Mutating the clone's ledgers must not affect the original (deep copy).
	cpuIdx := clone.VectorMap.GetIndex("cpu")
	clone.Zones[0].Available[cpuIdx] -= 1000
	clone.Zones[0].Allocatable[cpuIdx] -= 2000

	assert.Equal(t, int64(4), zoneAmount(orig.Zones[0].Available, orig.VectorMap, "cpu"), "original available ledger unchanged")
	assert.Equal(t, int64(4), zoneAmount(orig.Zones[0].Allocatable, orig.VectorMap, "cpu"), "original allocatable ledger unchanged")
	assert.Nil(t, (*NumaTopology)(nil).Clone())
}

func TestZoneIDIndexTranslation(t *testing.T) {
	topo := &NumaTopology{Zones: []*NumaZone{{ID: "node-0"}, {ID: "node-1"}}}

	idx, ok := topo.ZoneIndexByID("node-1")
	assert.True(t, ok)
	assert.Equal(t, 1, idx)

	_, ok = topo.ZoneIndexByID("node-9")
	assert.False(t, ok, "unknown id")

	id, ok := topo.ZoneID(0)
	assert.True(t, ok)
	assert.Equal(t, "node-0", id)

	_, ok = topo.ZoneID(5)
	assert.False(t, ok, "index out of range")
	_, ok = topo.ZoneID(-1)
	assert.False(t, ok, "negative index")
}

func TestNumaNodeID(t *testing.T) {
	tests := map[string]struct {
		expectedID int
		expectedOK bool
	}{
		"node-0":  {0, true},
		"node-13": {13, true},
		"socket":  {0, false},
		"":        {0, false},
	}
	for name, test := range tests {
		id, ok := numaNodeID(name)
		assert.Equal(t, test.expectedOK, ok, name)
		assert.Equal(t, test.expectedID, id, name)
	}
}

func TestZoneIndexByID(t *testing.T) {
	topo := &NumaTopology{Zones: []*NumaZone{{ID: "node-0"}, {ID: "node-1"}}}

	idx, ok := topo.ZoneIndexByID("node-1")
	assert.True(t, ok)
	assert.Equal(t, 1, idx)

	_, ok = topo.ZoneIndexByID("node-9")
	assert.False(t, ok, "unknown id")
}
