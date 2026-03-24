// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

func TestNewSubGroupInfo(t *testing.T) {
	name := "my-subgroup"
	topologyConstraint := &topology_info.TopologyConstraintInfo{}
	sgi := newSubGroupInfo(name, topologyConstraint)

	if sgi.name != name {
		t.Errorf("Expected name %s, got %s", name, sgi.name)
	}

	if sgi.topologyConstraint != topologyConstraint {
		t.Errorf("Expected topologyConstraint %v, got %v", topologyConstraint, sgi.topologyConstraint)
	}
}

func TestGetName(t *testing.T) {
	name := "test-subgroup"
	sgi := newSubGroupInfo(name, &topology_info.TopologyConstraintInfo{})

	if got := sgi.GetName(); got != name {
		t.Errorf("GetName() = %q, want %q", got, name)
	}
}

func TestGetTopologyConstraint(t *testing.T) {
	tc := &topology_info.TopologyConstraintInfo{}
	sgi := newSubGroupInfo("test-subgroup", tc)
	if sgi.GetTopologyConstraint() != tc {
		t.Error("AddTopologyConstraint() did not add the topology constraint")
	}
}
