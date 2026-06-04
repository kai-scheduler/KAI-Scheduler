// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

// numaNodeZone builds an NRT NUMA-node zone with the given per-resource Available quantities.
func numaNodeZone(name string, available map[string]string) nrtv1alpha2.Zone {
	var resources nrtv1alpha2.ResourceInfoList
	for resName, qty := range available {
		resources = append(resources, nrtv1alpha2.ResourceInfo{
			Name:      resName,
			Available: resource.MustParse(qty),
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
		expectedPolic tmPolicy
		expectedScope tmScope
	}{
		"attribute single-numa-node container": {
			nrt:           nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer, zone),
			expectedPolic: policySingleNUMANode,
			expectedScope: scopeContainer,
		},
		"attribute single-numa-node pod": {
			nrt:           nrtWithAttributes(policyValueSingleNUMANode, scopeValuePod, zone),
			expectedPolic: policySingleNUMANode,
			expectedScope: scopePod,
		},
		"attribute restricted, scope defaults to container when missing": {
			nrt:           nrtWithAttributes(policyValueRestricted, "", zone),
			expectedPolic: policyRestricted,
			expectedScope: scopeContainer,
		},
		"attribute best-effort": {
			nrt:           nrtWithAttributes(policyValueBestEffort, scopeValuePod, zone),
			expectedPolic: policyBestEffort,
			expectedScope: scopePod,
		},
		"attribute none": {
			nrt:           nrtWithAttributes(policyValueNone, scopeValueContainer, zone),
			expectedPolic: policyNone,
			expectedScope: scopeContainer,
		},
		"no attributes, no legacy policies -> none/container": {
			nrt:           nrtWithAttributes("", "", zone),
			expectedPolic: policyNone,
			expectedScope: scopeContainer,
		},
		"legacy SingleNUMANodePodLevel": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodePodLevel)},
			},
			expectedPolic: policySingleNUMANode,
			expectedScope: scopePod,
		},
		"legacy SingleNUMANodeContainerLevel": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodeContainerLevel)},
			},
			expectedPolic: policySingleNUMANode,
			expectedScope: scopeContainer,
		},
		"legacy Restricted": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				TopologyPolicies: []string{string(nrtv1alpha2.Restricted)},
			},
			expectedPolic: policyRestricted,
			expectedScope: scopeContainer,
		},
		"attributes take precedence over legacy": {
			nrt: &nrtv1alpha2.NodeResourceTopology{
				Zones:            nrtv1alpha2.ZoneList{zone},
				Attributes:       nrtv1alpha2.AttributeList{{Name: attrTopologyManagerPolicy, Value: policyValueRestricted}},
				TopologyPolicies: []string{string(nrtv1alpha2.SingleNUMANodePodLevel)},
			},
			expectedPolic: policyRestricted,
			expectedScope: scopeContainer,
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

func TestBuildNodeTopology(t *testing.T) {
	t.Run("nil NRT returns nil", func(t *testing.T) {
		assert.Nil(t, buildNodeTopology(nil, sets.New[v1.ResourceName]()))
	})

	t.Run("no NUMA-node zones returns nil", func(t *testing.T) {
		nrt := &nrtv1alpha2.NodeResourceTopology{
			Zones: nrtv1alpha2.ZoneList{{Name: "socket-0", Type: "Socket"}},
		}
		assert.Nil(t, buildNodeTopology(nrt, sets.New[v1.ResourceName]()))
	})

	t.Run("zones, availability and topologyAware are populated", func(t *testing.T) {
		nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
			numaNodeZone("node-0", map[string]string{"cpu": "4", "nvidia.com/gpu": "2"}),
			numaNodeZone("node-1", map[string]string{"cpu": "8", "memory": "16Gi"}),
			nrtv1alpha2.Zone{Name: "socket-0", Type: "Socket"}, // ignored
		)

		nt := buildNodeTopology(nrt, sets.New[v1.ResourceName]())

		assert.NotNil(t, nt)
		assert.Equal(t, policySingleNUMANode, nt.policy)
		assert.Len(t, nt.zones, 2, "only NUMA-node zones are kept")

		gpu := nt.zones[0].available["nvidia.com/gpu"]
		assert.Equal(t, int64(2), gpu.Value())

		assert.True(t, nt.topologyAware.HasAll("cpu", "memory", "nvidia.com/gpu"))
		assert.Equal(t, 3, nt.topologyAware.Len())
	})

	t.Run("denylisted resources are excluded from topologyAware", func(t *testing.T) {
		nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
			numaNodeZone("node-0", map[string]string{"cpu": "4", "memory": "16Gi", "nvidia.com/gpu": "2"}),
		)

		nt := buildNodeTopology(nrt, sets.New[v1.ResourceName]("memory"))

		assert.True(t, nt.topologyAware.HasAll("cpu", "nvidia.com/gpu"))
		assert.False(t, nt.topologyAware.Has("memory"), "denylisted resource excluded")
	})
}

func TestBuildNodeTopologyOrdersZones(t *testing.T) {
	// Deliberately out of order, and with a two-digit id to catch lexicographic ordering bugs
	// (node-10 must come after node-2).
	nrt := nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer,
		numaNodeZone("node-10", map[string]string{"cpu": "1"}),
		numaNodeZone("node-2", map[string]string{"cpu": "1"}),
		numaNodeZone("node-0", map[string]string{"cpu": "1"}),
	)

	nt := buildNodeTopology(nrt, sets.New[v1.ResourceName]())

	ids := []string{nt.zones[0].id, nt.zones[1].id, nt.zones[2].id}
	assert.Equal(t, []string{"node-0", "node-2", "node-10"}, ids)
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

func TestIsModeledPolicy(t *testing.T) {
	assert.True(t, (&nodeTopology{policy: policySingleNUMANode}).isModeledPolicy())
	assert.True(t, (&nodeTopology{policy: policyRestricted}).isModeledPolicy())
	assert.False(t, (&nodeTopology{policy: policyBestEffort}).isModeledPolicy())
	assert.False(t, (&nodeTopology{policy: policyNone}).isModeledPolicy())
}
