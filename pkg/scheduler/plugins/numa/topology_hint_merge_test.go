// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func TestTopologyAffinityRoundTrip(t *testing.T) {
	require.Zero(t, numaMask(nil))
	for _, indices := range [][]int{{0}, {0, 63}, {1, 3, 5}} {
		mask, err := pod_info.NewNUMAMask(indices)
		require.NoError(t, err)
		affinity := topologyAffinity(mask)
		require.Equal(t, indices, affinity.GetBits())
		require.Equal(t, mask, numaMask(affinity))
	}
}

func TestMergeTopologyHints(t *testing.T) {
	makeHint := func(nodes ...int) topologymanager.TopologyHint {
		mask, err := bitmask.NewBitMask(nodes...)
		require.NoError(t, err)
		return topologymanager.TopologyHint{NUMANodeAffinity: mask, Preferred: true}
	}
	mismatched := []map[string][]topologymanager.TopologyHint{
		{"cpu": {makeHint(0)}},
		{"memory": {makeHint(1)}},
	}
	wide := []map[string][]topologymanager.TopologyHint{{"memory": {makeHint(0, 1)}}}
	impossible := []map[string][]topologymanager.TopologyHint{{"memory": {}}}
	tests := []struct {
		name      string
		policy    node_info.TopologyManagerPolicy
		providers []map[string][]topologymanager.TopologyHint
		oneZone   bool
		nodes     []int
		preferred bool
		rejected  bool
	}{
		{name: "best effort admits mismatched preferred masks", policy: node_info.TopologyPolicyBestEffort, providers: mismatched, nodes: []int{0, 1}},
		{name: "restricted rejects mismatched preferred masks", policy: node_info.TopologyPolicyRestricted, providers: mismatched, rejected: true},
		{name: "restricted admits preferred wide mask", policy: node_info.TopologyPolicyRestricted, providers: wide, nodes: []int{0, 1}, preferred: true},
		{name: "single numa rejects preferred wide mask", policy: node_info.TopologyPolicySingleNUMANode, providers: wide, rejected: true},
		{name: "single numa admits matching singleton masks", policy: node_info.TopologyPolicySingleNUMANode, providers: []map[string][]topologymanager.TopologyHint{{"cpu": {makeHint(1)}, "memory": {makeHint(1)}}}, nodes: []int{1}, preferred: true},
		{name: "single numa on one zone returns no affinity", policy: node_info.TopologyPolicySingleNUMANode, providers: []map[string][]topologymanager.TopologyHint{{"memory": {makeHint(0)}}}, oneZone: true, preferred: true},
		{name: "no providers impose no constraints", policy: node_info.TopologyPolicyRestricted, nodes: []int{0, 1}, preferred: true},
		{name: "nil hints impose no constraints", policy: node_info.TopologyPolicyRestricted, providers: []map[string][]topologymanager.TopologyHint{{"memory": nil}}, nodes: []int{0, 1}, preferred: true},
		{name: "single numa admits no care", policy: node_info.TopologyPolicySingleNUMANode, providers: []map[string][]topologymanager.TopologyHint{{"memory": {{Preferred: true}}}}, preferred: true},
		{name: "best effort admits impossible hints", policy: node_info.TopologyPolicyBestEffort, providers: impossible, nodes: []int{0, 1}},
		{name: "restricted rejects impossible hints", policy: node_info.TopologyPolicyRestricted, providers: impossible, rejected: true},
		{name: "single numa rejects impossible hints", policy: node_info.TopologyPolicySingleNUMANode, providers: impossible, rejected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			topology := twoZoneNode(test.policy, req("memory", "100Gi"))
			if test.oneZone {
				topology = numaTopology(test.policy, node_info.TopologyScopeContainer, numaZone("node-0", map[string]string{"memory": "100Gi"}))
			}
			hint, err := mergeTopologyHints(topology, test.providers)
			if test.rejected {
				require.ErrorIs(t, err, errNotNumaAligned)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.preferred, hint.Preferred)
			if test.nodes == nil {
				require.Nil(t, hint.NUMANodeAffinity)
				return
			}
			require.NotNil(t, hint.NUMANodeAffinity)
			require.Equal(t, test.nodes, hint.NUMANodeAffinity.GetBits())
		})
	}
}

func TestMemorySolverSingleNUMAOnOneZone(t *testing.T) {
	topology := numaTopology(node_info.TopologyPolicySingleNUMANode, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{"memory": "100Gi"}))
	topology.MemoryGroups = &node_info.MemoryGroupState{Status: node_info.MemoryGroupsKnown}
	plugin, _, node := wiredPlugin(topology)
	placement, err := plugin.evaluate(makeGuaranteedTask("single-zone", map[string]string{"memory": "40Gi"}), node)
	require.NoError(t, err)
	require.Len(t, placement.MemoryGroups, 1)
	require.Equal(t, []int{0}, placement.MemoryGroups[0].Mask.Indices())
	require.Equal(t, req("memory", "40Gi"), placement.MemoryGroups[0].Amount)
}

func TestBestEffortPreferredFirstMatchesFullMerge(t *testing.T) {
	makeHint := func(preferred bool, nodes ...int) topologymanager.TopologyHint {
		mask, err := bitmask.NewBitMask(nodes...)
		require.NoError(t, err)
		return topologymanager.TopologyHint{NUMANodeAffinity: mask, Preferred: preferred}
	}
	tests := []struct {
		name      string
		zones     []node_info.NumaZoneSpec
		providers []map[string][]topologymanager.TopologyHint
		preferred bool
		want      map[int]v1.ResourceList
	}{
		{
			name: "disjoint preferred masks require full nonpreferred fallback",
			zones: []node_info.NumaZoneSpec{
				numaZone("node-0", map[string]string{"cpu": "4"}),
				numaZone("node-1", map[string]string{gpu: "1"}),
			},
			providers: []map[string][]topologymanager.TopologyHint{
				{"cpu": {makeHint(true, 0), makeHint(false, 0, 1)}},
				{gpu: {makeHint(true, 1), makeHint(false, 0, 1)}},
			},
			want: map[int]v1.ResourceList{0: req("cpu", "4"), 1: req(gpu, "1")},
		},
		{
			name: "common preferred masks retain upstream lowest-zone tie break",
			zones: []node_info.NumaZoneSpec{
				numaZone("node-0", map[string]string{"cpu": "4", gpu: "1"}),
				numaZone("node-1", map[string]string{"cpu": "4", gpu: "1"}),
			},
			providers: []map[string][]topologymanager.TopologyHint{
				{"cpu": {makeHint(true, 0), makeHint(true, 1), makeHint(false, 0, 1)}},
				{gpu: {makeHint(true, 0), makeHint(true, 1), makeHint(false, 0, 1)}},
			},
			preferred: true,
			want:      map[int]v1.ResourceList{0: req("cpu", "4", gpu, "1")},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			topology := numaTopology(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer, test.zones...)
			plugin, _, node := wiredPlugin(topology)
			task := makeGuaranteedTask("preferred-first", map[string]string{"cpu": "4", gpu: "1"})
			request := resource_info.NewResourceVectorFromResourceList(req("cpu", "4", gpu, "1"), topology.VectorMap)
			state := &evaluationState{topology: topology, zoneResources: topology.AwareIndices, consumed: make([]float64, len(topology.Zones)*topology.VectorMap.Len())}
			hint, err := state.mergedHint(request)
			require.NoError(t, err)
			fullHint, err := mergeTopologyHints(topology, test.providers)
			require.NoError(t, err)
			require.Equal(t, test.preferred, hint.Preferred)
			require.Equal(t, fullHint.Preferred, hint.Preferred)
			require.Equal(t, fullHint.NUMANodeAffinity.GetBits(), hint.NUMANodeAffinity.GetBits())
			require.Equal(t, []int{0}, hint.NUMANodeAffinity.GetBits())
			placement, err := plugin.evaluate(task, node)
			require.NoError(t, err)
			assertPlacement(t, placement, test.want)
		})
	}
}
