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
