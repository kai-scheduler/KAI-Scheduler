// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology_info

import (
	"testing"

	"gotest.tools/assert"
)

func TestTopologyConstraintInfo_GetSchedulingConstraintsSignature(t *testing.T) {
	tests := []struct {
		name        string
		tcA         *TopologyConstraintInfo
		tcB         *TopologyConstraintInfo
		expectEqual bool
	}{
		{
			name:        "equal",
			tcA:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			tcB:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			expectEqual: true,
		},
		{
			name:        "different topology",
			tcA:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			tcB:         &TopologyConstraintInfo{Topology: "topo2", RequiredLevel: "rack", PreferredLevel: "zone"},
			expectEqual: false,
		},
		{
			name:        "different required level",
			tcA:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			tcB:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack2", PreferredLevel: "zone"},
			expectEqual: false,
		},
		{
			name:        "different preferred level",
			tcA:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			tcB:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone2"},
			expectEqual: false,
		},
		// swap preferred and required level
		{
			name:        "swapped preferred and required level",
			tcA:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack", PreferredLevel: "zone"},
			tcB:         &TopologyConstraintInfo{Topology: "topo", RequiredLevel: "zone", PreferredLevel: "rack"},
			expectEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tcA := tt.tcA
			tcB := tt.tcB
			assert.Equal(t, tt.expectEqual, tcA.GetSchedulingConstraintsSignature() == tcB.GetSchedulingConstraintsSignature())
		})
	}
}

func TestResolveAliases(t *testing.T) {
	aliases := map[string]string{
		"rack": "accelerator.nvidia.com/rack",
		"node": "kubernetes.io/hostname",
	}

	tc := &TopologyConstraintInfo{Topology: "network", RequiredLevel: "rack", PreferredLevel: "node"}
	tc.ResolveAliases(aliases)

	assert.Equal(t, "accelerator.nvidia.com/rack", tc.RequiredLevel)
	assert.Equal(t, "kubernetes.io/hostname", tc.PreferredLevel)
	assert.Equal(t, "network", tc.Topology)
}

func TestResolveAliases_PassThroughAndPartial(t *testing.T) {
	aliases := map[string]string{"rack": "accelerator.nvidia.com/rack"}

	tc := &TopologyConstraintInfo{
		Topology:       "network",
		RequiredLevel:  "accelerator.nvidia.com/rack", // raw label, not an alias key
		PreferredLevel: "",                            // empty stays empty
	}
	tc.ResolveAliases(aliases)

	assert.Equal(t, "accelerator.nvidia.com/rack", tc.RequiredLevel)
	assert.Equal(t, "", tc.PreferredLevel)
}

func TestResolveAliases_InvalidatesSignature(t *testing.T) {
	aliases := map[string]string{"rack": "accelerator.nvidia.com/rack"}
	tc := &TopologyConstraintInfo{Topology: "network", RequiredLevel: "rack"}

	before := tc.GetSchedulingConstraintsSignature() // computed from the alias
	tc.ResolveAliases(aliases)
	after := tc.GetSchedulingConstraintsSignature() // recomputed from the canonical label

	assert.Assert(t, before != after, "signature must be recomputed from the canonical level")
}

func TestResolveAliases_NilSafeAndNoAliases(t *testing.T) {
	var nilTC *TopologyConstraintInfo
	nilTC.ResolveAliases(map[string]string{"rack": "x"}) // must not panic

	tc := &TopologyConstraintInfo{Topology: "network", RequiredLevel: "rack"}
	tc.ResolveAliases(nil)
	assert.Equal(t, "rack", tc.RequiredLevel) // no aliases configured ⇒ unchanged
}
