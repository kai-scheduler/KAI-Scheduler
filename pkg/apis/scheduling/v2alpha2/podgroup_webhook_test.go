// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2alpha2

import (
	"context"
	"errors"
	"testing"

	"k8s.io/utils/ptr"
)

func TestValidateSubGroups(t *testing.T) {
	tests := []struct {
		name      string
		subGroups []SubGroup
		wantErr   *validationErrors
	}{
		{
			name: "Valid DAG single root",
			subGroups: []SubGroup{
				{Name: "a", MinSubGroup: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinSubGroup: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
			},
			wantErr: nil,
		},
		{
			name: "Valid DAG multiple roots",
			subGroups: []SubGroup{
				{Name: "a", MinSubGroup: ptr.To(int32(1))},
				{Name: "b", MinSubGroup: ptr.To(int32(1))},
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
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "parent x of b was not found"}},
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
			wantErr: &validationErrors{minDefinitionErrors: []error{&missingMinMemberError{msg: "subgroup a: minMember is required"}}},
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
			wantErr: &validationErrors{minDefinitionErrors: []error{&missingMinMemberError{msg: "subgroup l: minMember is required"}}},
		},
		{
			name: "Duplicate subgroup names",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "a", MinMember: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "duplicate subgroup name a"}},
		},
		{
			name: "Cycle in graph (a -> b -> c -> a) - duplicate subgroup name",
			subGroups: []SubGroup{
				{Name: "a", MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
				{Name: "a", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "duplicate subgroup name a"}}, // duplicate is caught before cycle
		},
		{
			name: "Self-parent subgroup (cycle of length 1)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "cycle detected in subgroups"}},
		},
		{
			name: "Cycle in graph (a -> b -> c -> a)",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "cycle detected in subgroups"}},
		},
		{
			name: "Multiple disjoint cycles",
			subGroups: []SubGroup{
				{Name: "a", Parent: ptr.To("b"), MinMember: ptr.To(int32(1))},
				{Name: "b", Parent: ptr.To("a"), MinMember: ptr.To(int32(1))},
				{Name: "c", Parent: ptr.To("d"), MinMember: ptr.To(int32(1))},
				{Name: "d", Parent: ptr.To("c"), MinMember: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{structuralError: &subGroupGraphError{msg: "cycle detected in subgroups"}},
		},
		// minSubGroup on SubGroup tests
		{
			name: "Valid: mid-level SubGroup uses minSubGroup",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(2))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: leaf SubGroup uses minSubGroup",
			subGroups: []SubGroup{
				{Name: "A", MinSubGroup: ptr.To(int32(1))},
			},
			wantErr: &validationErrors{minDefinitionErrors: []error{
				&invalidMinSubGroupError{msg: `subgroup "A": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`},
				&missingMinMemberError{msg: "subgroup A: minMember is required"},
			}},
		},
		{
			name: "Invalid: mid-level SubGroup uses minMember",
			subGroups: []SubGroup{
				{Name: "parent", MinMember: ptr.To(int32(2))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: &validationErrors{minDefinitionErrors: []error{&parentMinMemberError{msg: `subgroup "parent": minMember cannot be set on a mid-level SubGroup (has child SubGroups); use minSubGroup instead`}}},
		},
		{
			name: "Invalid: SubGroup has both minMember and minSubGroup",
			subGroups: []SubGroup{
				{Name: "parent", MinMember: ptr.To(int32(2)), MinSubGroup: ptr.To(int32(1))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: &validationErrors{structuralError: &mutuallyExclusiveFieldsError{msg: `subgroup "parent": minMember and minSubGroup are mutually exclusive`}},
		},
		{
			name: "Invalid: SubGroup minSubGroup exceeds child count",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(3))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: &validationErrors{minDefinitionErrors: []error{&minSubGroupExceedsChildCountError{msg: `subgroup "parent": minSubGroup (3) exceeds the number of direct child SubGroups (2)`}}},
		},
		{
			name: "Valid: minSubGroup equals child count",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(2))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: nil,
		},
		{
			name: "Invalid: minSubGroup = 0 on SubGroup with children",
			subGroups: []SubGroup{
				{Name: "parent", MinSubGroup: ptr.To(int32(0))},
				{Name: "child-1", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
				{Name: "child-2", Parent: ptr.To("parent"), MinMember: ptr.To(int32(4))},
			},
			wantErr: &validationErrors{minDefinitionErrors: []error{&invalidMinSubGroupError{msg: `subgroup "parent": minSubGroup must be greater than 0`}}},
		},
		{
			name: "Invalid: minSubGroup = 0 on leaf SubGroup",
			subGroups: []SubGroup{
				{Name: "A", MinSubGroup: ptr.To(int32(0))},
			},
			wantErr: &validationErrors{minDefinitionErrors: []error{
				&invalidMinSubGroupError{msg: `subgroup "A": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`},
				&missingMinMemberError{msg: "subgroup A: minMember is required"},
			}},
		},
		{
			name: "Valid: 2-level hierarchy with minSubGroup at both levels",
			subGroups: []SubGroup{
				{Name: "decode", MinSubGroup: ptr.To(int32(2))},
				{Name: "decode-leaders", Parent: ptr.To("decode"), MinMember: ptr.To(int32(1))},
				{Name: "decode-workers", Parent: ptr.To("decode"), MinMember: ptr.To(int32(4))},
				{Name: "prefill", MinSubGroup: ptr.To(int32(2))},
				{Name: "prefill-leaders", Parent: ptr.To("prefill"), MinMember: ptr.To(int32(1))},
				{Name: "prefill-workers", Parent: ptr.To("prefill"), MinMember: ptr.To(int32(4))},
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSubGroups(tt.subGroups)
			want := tt.wantErr
			if want == nil {
				want = &validationErrors{}
			}
			if !errorsEqual(want.structuralError, got.structuralError) {
				t.Fatalf("expected structural error %v, got %v", want.structuralError, got.structuralError)
			}
			if !errorsListEqual(want.minDefinitionErrors, got.minDefinitionErrors) {
				t.Fatalf("expected min definition errors %v, got %v", want.minDefinitionErrors, got.minDefinitionErrors)
			}
		})
	}
}

func TestValidatePodGroupSpec(t *testing.T) {
	tests := []struct {
		name string
		spec PodGroupSpec
		want *validationErrors
	}{
		{
			name: "Valid: minMember only, no subgroups",
			spec: PodGroupSpec{
				MinMember: ptr.To(int32(4)),
			},
			want: nil,
		},
		{
			name: "Valid: minSubGroup with leaf subgroups using minMember",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(3)),
				SubGroups: []SubGroup{
					{Name: "prefill-0", MinMember: ptr.To(int32(8))},
					{Name: "prefill-1", MinMember: ptr.To(int32(8))},
					{Name: "prefill-2", MinMember: ptr.To(int32(8))},
					{Name: "prefill-3", MinMember: ptr.To(int32(8))},
				},
			},
			want: nil,
		},
		{
			name: "Valid: minSubGroup equals subgroup count",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(2)),
				SubGroups: []SubGroup{
					{Name: "a", MinMember: ptr.To(int32(4))},
					{Name: "b", MinMember: ptr.To(int32(4))},
				},
			},
			want: nil,
		},
		{
			name: "Invalid: both minMember and minSubGroup set on PodGroup",
			spec: PodGroupSpec{
				MinMember:   ptr.To(int32(24)),
				MinSubGroup: ptr.To(int32(3)),
				SubGroups: []SubGroup{
					{Name: "a", MinMember: ptr.To(int32(8))},
					{Name: "b", MinMember: ptr.To(int32(8))},
					{Name: "c", MinMember: ptr.To(int32(8))},
				},
			},
			want: &validationErrors{structuralError: &mutuallyExclusiveFieldsError{msg: "minMember and minSubGroup are mutually exclusive: set minMember (24) to schedule a fixed number of pods, or set minSubGroup to require a minimum number of child SubGroups, but not both"}},
		},
		{
			name: "Invalid: minSubGroup exceeds root subgroup count",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(5)),
				SubGroups: []SubGroup{
					{Name: "a", MinMember: ptr.To(int32(8))},
					{Name: "b", MinMember: ptr.To(int32(8))},
					{Name: "c", MinMember: ptr.To(int32(8))},
					{Name: "d", MinMember: ptr.To(int32(8))},
				},
			},
			want: &validationErrors{minDefinitionErrors: []error{&minSubGroupExceedsChildCountError{msg: "minSubGroup (5) exceeds the number of direct child SubGroups (4)"}}},
		},
		{
			name: "Valid: 2-level hierarchy",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(2)),
				SubGroups: []SubGroup{
					{Name: "decode", MinSubGroup: ptr.To(int32(2))},
					{Name: "decode-leaders", Parent: ptr.To("decode"), MinMember: ptr.To(int32(1))},
					{Name: "decode-workers", Parent: ptr.To("decode"), MinMember: ptr.To(int32(4))},
					{Name: "prefill", MinSubGroup: ptr.To(int32(2))},
					{Name: "prefill-leaders", Parent: ptr.To("prefill"), MinMember: ptr.To(int32(1))},
					{Name: "prefill-workers", Parent: ptr.To("prefill"), MinMember: ptr.To(int32(4))},
				},
			},
			want: nil,
		},
		{
			name: "Invalid: subgroup validation error propagates",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(1)),
				SubGroups: []SubGroup{
					{Name: "leaf", MinSubGroup: ptr.To(int32(1))},
				},
			},
			want: &validationErrors{minDefinitionErrors: []error{
				&invalidMinSubGroupError{msg: `subgroup "leaf": minSubGroup cannot be set on a leaf SubGroup (no child SubGroups)`},
				&missingMinMemberError{msg: "subgroup leaf: minMember is required"},
			}},
		},
		{
			name: "Valid: no subgroups, no minMember, no minSubGroup",
			spec: PodGroupSpec{},
			want: nil,
		},
		{
			name: "Invalid: minSubGroup with no subgroups defined",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(1)),
			},
			want: &validationErrors{minDefinitionErrors: []error{&minSubGroupExceedsChildCountError{msg: "minSubGroup (1) exceeds the number of direct child SubGroups (0)"}}},
		},
		{
			name: "Invalid: minSubGroup = 0 on PodGroup",
			spec: PodGroupSpec{
				MinSubGroup: ptr.To(int32(0)),
				SubGroups: []SubGroup{
					{Name: "a", MinMember: ptr.To(int32(4))},
					{Name: "b", MinMember: ptr.To(int32(4))},
				},
			},
			want: &validationErrors{minDefinitionErrors: []error{&invalidMinSubGroupError{msg: "minSubGroup at the podgroup level must be equal to or greater than 1"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validatePodGroupSpec(&tt.spec)
			want := tt.want
			if want == nil {
				want = &validationErrors{}
			}
			if !errorsEqual(want.structuralError, got.structuralError) {
				t.Fatalf("expected structural error %v, got %v", want.structuralError, got.structuralError)
			}
			if !errorsListEqual(want.minDefinitionErrors, got.minDefinitionErrors) {
				t.Fatalf("expected min definition errors %v, got %v", want.minDefinitionErrors, got.minDefinitionErrors)
			}
		})
	}
}

// TestCreateRejectsWhatUpdateWarns verifies that parentMinMemberError (minMember on a non-leaf subgroup)
// causes ValidateCreate to fail hard while ValidateUpdate downgrades it to a warning.
func TestCreateRejectsWhatUpdateWarns(t *testing.T) {
	podGroup := &PodGroup{
		Spec: PodGroupSpec{
			SubGroups: []SubGroup{
				{Name: "parent", MinMember: ptr.To(int32(2))},
				{Name: "child", Parent: ptr.To("parent"), MinMember: ptr.To(int32(1))},
			},
		},
	}

	wantMsg := (&parentMinMemberError{msg: `subgroup "parent": minMember cannot be set on a mid-level SubGroup (has child SubGroups); use minSubGroup instead`}).Error()

	validator := &PodGroup{}
	ctx := context.Background()

	_, createErr := validator.ValidateCreate(ctx, podGroup)
	if createErr == nil {
		t.Fatal("ValidateCreate should fail for parentMinMemberError")
	}
	var pme *parentMinMemberError
	if !errors.As(createErr, &pme) {
		t.Fatalf("ValidateCreate error should unwrap to parentMinMemberError, got: %v", createErr)
	}
	if pme.Error() != wantMsg {
		t.Fatalf("ValidateCreate: got message %q, want %q", pme.Error(), wantMsg)
	}

	warnings, updateErr := validator.ValidateUpdate(ctx, podGroup, podGroup)
	if updateErr != nil {
		t.Fatalf("ValidateUpdate should succeed, got: %v", updateErr)
	}
	found := false
	for _, w := range warnings {
		if w == wantMsg {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ValidateUpdate should warn with parentMinMemberError message; warnings=%v", warnings)
	}
}

func errorsEqual(a, b error) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Error() == b.Error()
	}
}

func errorsListEqual(want, got []error) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if !errorsEqual(want[i], got[i]) {
			return false
		}
	}
	return true
}
