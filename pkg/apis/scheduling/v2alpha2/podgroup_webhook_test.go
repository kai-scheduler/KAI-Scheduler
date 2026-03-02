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
		// minSubGroup on SubGroup tests
		{
			name: "Valid: mid-level SubGroup uses minSubGroup",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(2))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: leaf SubGroup uses minSubGroup",
			subGroups: []SubGroup{
				{Name: "A", MinSubGroup: ptr.To(int32(1))},
			},
			wantErr: errors.New(`subgroup "A": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`),
		},
		{
			name: "Invalid: mid-level SubGroup uses minMember",
			subGroups: []SubGroup{
				{Name: "parent", MinMember: 2},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: errors.New(`subgroup "parent": minMember cannot be set on a mid-level SubGroup (has child SubGroups); use minSubGroup instead`),
		},
		{
			name: "Invalid: SubGroup has both minMember and minSubGroup",
			subGroups: []SubGroup{
				{Name: "parent", MinMember: 2, MinSubGroup: ptr.To(int32(1))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: errors.New(`subgroup "parent": minMember and minSubGroup are mutually exclusive`),
		},
		{
			name: "Invalid: SubGroup minSubGroup exceeds child count",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(3))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: errors.New(`subgroup "parent": minSubGroup (3) exceeds the number of direct child SubGroups (2)`),
		},
		{
			name: "Valid: minSubGroup equals child count",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(2))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: minSubGroup = 0 on SubGroup with children",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(0))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: 4},
			},
			wantErr: errors.New(`subgroup "parent": minSubGroup must be greater than 0`),
		},
		{
			name: "Invalid: minSubGroup = 0 on leaf SubGroup",
			subGroups: []SubGroup{
				{Name: "A", MinSubGroup: ptr.To(int32(0))},
			},
			wantErr: errors.New(`subgroup "A": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`),
		},
		{
			name: "Valid: 2-level hierarchy with minSubGroup at both levels",
			subGroups: []SubGroup{
				{Name: "decode", MinSubGroup: ptr.To(int32(2))},
				{Name: "decode-leaders", Parent: ptr.To("decode"), MinMember: 1},
				{Name: "decode-workers", Parent: ptr.To("decode"), MinMember: 4},
				{Name: "prefill", MinSubGroup: ptr.To(int32(2))},
				{Name: "prefill-leaders", Parent: ptr.To("prefill"), MinMember: 1},
				{Name: "prefill-workers", Parent: ptr.To("prefill"), MinMember: 4},
			},
			wantErr: nil,
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

func TestValidatePodGroupSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    PodGroupSpec
		wantErr error
	}{
		{
			name: "Valid: minMember only, no subgroups",
			spec: PodGroupSpec{
				MinMember: 4,
			},
			wantErr: nil,
		},
		{
			name: "Valid: minSubGroup with leaf subgroups using minMember",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(3)),
				SubGroups: []SubGroup{
					{Name: "prefill-0", MinMember: 8},
					{Name: "prefill-1", MinMember: 8},
					{Name: "prefill-2", MinMember: 8},
					{Name: "prefill-3", MinMember: 8},
				},
			},
			wantErr: nil,
		},
		{
			name: "Valid: minSubGroup equals subgroup count",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(2)),
				SubGroups: []SubGroup{
					{Name: "A", MinMember: 4},
					{Name: "B", MinMember: 4},
				},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: both minMember and minSubGroup set on PodGroup",
			spec: PodGroupSpec{
				MinMember:   24,
				MinSubGroup: ptr.To(int32(3)),
				SubGroups: []SubGroup{
					{Name: "A", MinMember: 8},
					{Name: "B", MinMember: 8},
					{Name: "C", MinMember: 8},
				},
			},
			wantErr: errors.New("minMember and minSubGroup are mutually exclusive: set minMember (24) to schedule a fixed number of pods, or set minSubGroup to require a minimum number of child SubGroups, but not both"),
		},
		{
			name: "Invalid: minSubGroup exceeds root subgroup count",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(5)),
				SubGroups: []SubGroup{
					{Name: "A", MinMember: 8},
					{Name: "B", MinMember: 8},
					{Name: "C", MinMember: 8},
					{Name: "D", MinMember: 8},
				},
			},
			wantErr: errors.New("minSubGroup (5) exceeds the number of direct child SubGroups (4)"),
		},
		{
			name: "Valid: 2-level hierarchy",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(2)),
				SubGroups: []SubGroup{
					{Name: "decode", MinSubGroup: ptr.To(int32(2))},
					{Name: "decode-leaders", Parent: ptr.To("decode"), MinMember: 1},
					{Name: "decode-workers", Parent: ptr.To("decode"), MinMember: 4},
					{Name: "prefill", MinSubGroup: ptr.To(int32(2))},
					{Name: "prefill-leaders", Parent: ptr.To("prefill"), MinMember: 1},
					{Name: "prefill-workers", Parent: ptr.To("prefill"), MinMember: 4},
				},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: subgroup validation error propagates",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(1)),
				SubGroups: []SubGroup{
					{Name: "leaf", MinSubGroup: ptr.To(int32(1))}, // leaf with minSubGroup is invalid
				},
			},
			wantErr: errors.New(`subgroup "leaf": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`),
		},
		{
			name: "Valid: no subgroups, no minMember, no minSubGroup",
			spec: PodGroupSpec{},
			wantErr: nil,
		},
		{
			name: "Invalid: minSubGroup with no subgroups defined",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(1)),
			},
			wantErr: errors.New("minSubGroup (1) exceeds the number of direct child SubGroups (0)"),
		},
		{
			name: "Invalid: minSubGroup = 0 on PodGroup",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(0)),
				SubGroups: []SubGroup{
					{Name: "A", MinMember: 4},
					{Name: "B", MinMember: 4},
				},
			},
			wantErr: errors.New("minSubGroup must be greater than 0"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePodGroupSpec(&tt.spec)
			if (err != nil && tt.wantErr == nil) || (err == nil && tt.wantErr != nil) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if err != nil && tt.wantErr != nil && err.Error() != tt.wantErr.Error() {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
