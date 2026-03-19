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
				{Name: "a", MinMember: 1},
				{Name: "b", Parent: ptr.To("a"), MinMember: 1},
				{Name: "c", Parent: ptr.To("b"), MinMember: 1},
			},
			wantErr: nil,
		},
		{
			name: "Valid DAG multiple roots",
			subGroups: []SubGroup{
				{Name: "a", MinMember: 1},
				{Name: "b", MinMember: 1},
				{Name: "c", Parent: ptr.To("a"), MinMember: 1},
				{Name: "d", Parent: ptr.To("b"), MinMember: 1},
			},
			wantErr: nil,
		},
		{
			name: "Missing parent",
			subGroups: []SubGroup{
				{Name: "a", MinMember: 1},
				{Name: "b", Parent: ptr.To("x"), MinMember: 1}, // parent x does not exist
			},
			wantErr: errors.New("parent x of b was not found"),
		},
		{
			name:      "Empty list",
			subGroups: []SubGroup{},
			wantErr:   nil,
		},
		{
			name: "Duplicate subgroup names",
			subGroups: []SubGroup{
				{Name: "a", MinMember: 1},
				{Name: "a", MinMember: 1}, // duplicate
			},
			wantErr: errors.New("duplicate subgroup name a"),
		},
		{
			name: "Cycle in graph (a -> b -> c -> a) - duplicate subgroup name",
			subGroups: []SubGroup{
				{Name: "a", MinMember: 1},
				{Name: "b", Parent: ptr.To("a"), MinMember: 1},
				{Name: "c", Parent: ptr.To("b"), MinMember: 1},
				{Name: "a", Parent: ptr.To("c"), MinMember: 1}, // creates a cycle
			},
			wantErr: errors.New("duplicate subgroup name a"), // duplicate is caught before cycle
		},
		{
			name: "Self-parent subgroup (cycle of length 1)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("a"), MinMember: 1},
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Cycle in graph (a -> b -> c -> a)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("c"), MinMember: 1},
				{Name: "b", Parent: ptr.To("a"), MinMember: 1},
				{Name: "c", Parent: ptr.To("b"), MinMember: 1}, // creates a cycle
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Multiple disjoint cycles",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("b"), MinMember: 1},
				{Name: "b", Parent: ptr.To("a"), MinMember: 1}, // cycle a <-> b
				{Name: "c", Parent: ptr.To("d"), MinMember: 1},
				{Name: "d", Parent: ptr.To("c"), MinMember: 1}, // cycle c <-> d
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
