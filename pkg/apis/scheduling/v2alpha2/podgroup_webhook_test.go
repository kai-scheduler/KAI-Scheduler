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
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
			},
			wantErr: nil,
		},
		{
			name: "Valid DAG multiple roots",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "b", MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "d", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
			},
			wantErr: nil,
		},
		{
			name: "Missing parent",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("x"), MinMember: ptr.To(int32(1))},
			},
			wantErr: errors.New("parent x of b was not found"),
		},
		{
			name:      "Empty list",
			subGroups: []SubGroup{},
			wantErr:   nil,
		},
		{
			name: "nil minMember on leaf subgroup",
			subGroups: []SubGroup{
				{Name: "a"},
			},
			wantErr: errors.New("subgroup a: minMember is required"),
		},
		{
			name: "parent subgroup may omit minMember when it has subgroup children",
			subGroups: []SubGroup{
				{Name: "p"},
				{Name: "l", Parent: ptr.To("p"), MinMember: ptr.To(int32(1))},
			},
			wantErr: nil,
		},
		{
			name: "leaf child still requires minMember when parent omits it",
			subGroups: []SubGroup{
				{Name: "p"},
				{Name: "l", Parent: ptr.To("p")},
			},
			wantErr: errors.New("subgroup l: minMember is required"),
		},
		{
			name: "Duplicate subgroup names",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "a", MinMember: ptr.To(int32(1))},
			},
			wantErr: errors.New("duplicate subgroup name a"),
		},
		{
			name: "Cycle in graph (a -> b -> c -> a) - duplicate subgroup name",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
				{Name: "a", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
			},
			wantErr: errors.New("duplicate subgroup name a"),
		},
		{
			name: "Self-parent subgroup (cycle of length 1)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Cycle in graph (a -> b -> c -> a)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
			},
			wantErr: errors.New("cycle detected in subgroups"),
		},
		{
			name: "Multiple disjoint cycles",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("d"), MinMember: ptr.To(int32(1))},
				{Name: "d", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
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
