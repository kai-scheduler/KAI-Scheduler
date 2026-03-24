// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

import (
	"errors"
	"testing"

	"k8s.io/utils/ptr"
)

func TestValidateSubGroups(t *testing.T) {
	tests := []struct {
		name      string
		subGroups []SubGroup
		wantErr   error
	}{
		{
			name: "Valid DAG single root",
			subGroups: []SubGroup{
				{Name: "A", MinMember: 1},
				{Name: "B", Parent: ptr.To("A"), MinMember: 1},
				{Name: "C", Parent: ptr.To("B"), MinMember: 1},
			},
			wantErr: nil,
		},
		{
			name: "Valid DAG multiple roots",
			subGroups: []SubGroup{
				{Name: "A", MinMember: 1},
				{Name: "B", MinMember: 1},
				{Name: "C", Parent: ptr.To("A"), MinMember: 1},
				{Name: "D", Parent: ptr.To("B"), MinMember: 1},
			},
			wantErr: nil,
		},
		{
			name: "Missing parent",
			subGroups: []SubGroup{
				{Name: "A", MinMember: 1},
				{Name: "B", Parent: ptr.To("X"), MinMember: 1}, // parent X does not exist
			},
			wantErr: errors.New("parent X of B was not found"),
		},
		{
			name:      "Empty list",
			subGroups: []SubGroup{},
			wantErr:   nil,
		},
		{
			name: "Duplicate subgroup names",
			subGroups: []SubGroup{
				{Name: "A", MinMember: 1},
				{Name: "A", MinMember: 1}, // duplicate
			},
			wantErr: errors.New("duplicate subgroup name A"),
		},
		{
			name: "Cycle in graph (A -> B -> C -> A) - duplicate subgroup name",
			subGroups: []SubGroup{
				{Name: "A", MinMember: 1},
				{Name: "B", Parent: ptr.To("A"), MinMember: 1},
				{Name: "C", Parent: ptr.To("B"), MinMember: 1},
				{Name: "A", Parent: ptr.To("C"), MinMember: 1}, // creates a cycle
			},
			wantErr: errors.New("duplicate subgroup name A"), // duplicate is caught before cycle
		},
		{
			name: "Self-parent subgroup (cycle of length 1)",
			subGroups: []SubGroup{
				{Name: "A", Parent: ptr.To("A"), MinMember: 1},
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Cycle in graph (A -> B -> C -> A)",
			subGroups: []SubGroup{
				{Name: "A", Parent: ptr.To("C"), MinMember: 1},
				{Name: "B", Parent: ptr.To("A"), MinMember: 1},
				{Name: "C", Parent: ptr.To("B"), MinMember: 1}, // creates a cycle
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Multiple disjoint cycles",
			subGroups: []SubGroup{
				{Name: "A", Parent: ptr.To("B"), MinMember: 1},
				{Name: "B", Parent: ptr.To("A"), MinMember: 1}, // cycle A <-> B
				{Name: "C", Parent: ptr.To("D"), MinMember: 1},
				{Name: "D", Parent: ptr.To("C"), MinMember: 1}, // cycle C <-> D
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubGroups(tt.subGroups)
			if (err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if err != nil && tt.wantErr != nil && err.Error() != tt.wantErr.Error() {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
