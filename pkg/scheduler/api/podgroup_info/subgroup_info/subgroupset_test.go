// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"reflect"
	"testing"
)

func newTestPodSet(name string, minAvailable int32) *PodSet {
	return &PodSet{
		SubGroupInfo:   *newSubGroupInfo(name, nil),
		minAvailable:   minAvailable,
		podInfos:       nil,
		podStatusIndex: nil,
		podStatusMap:   nil,
	}
}

func TestNewSubGroupSet(t *testing.T) {
	sg := NewSubGroupSet("root", nil)
	if sg.GetName() != "root" {
		t.Errorf("expected name to be 'root', got %s", sg.GetName())
	}
	if len(sg.GetChildGroups()) != 0 {
		t.Errorf("expected 0 child groups, got %d", len(sg.GetChildGroups()))
	}
	if len(sg.GetChildPodSets()) != 0 {
		t.Errorf("expected 0 podsets, got %d", len(sg.GetChildPodSets()))
	}
}

func TestAddSubGroup(t *testing.T) {
	parent := NewSubGroupSet("parent", nil)
	child := NewSubGroupSet("child", nil)
	parent.AddSubGroup(child)
	groups := parent.GetChildGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 child group, got %d", len(groups))
	}
	if groups[0] != child {
		t.Errorf("expected added child to be present")
	}
}

func TestAddPodSet(t *testing.T) {
	parent := NewSubGroupSet("parent", nil)
	podSet := newTestPodSet("podset", 2)
	parent.AddPodSet(podSet)
	ps := parent.GetChildPodSets()
	if len(ps) != 1 {
		t.Fatalf("expected 1 podset, got %d", len(ps))
	}
	if ps[0] != podSet {
		t.Errorf("did not get correct podset after adding")
	}
}

func TestClone_OnlyPodSets(t *testing.T) {
	root := NewSubGroupSet("root", nil)
	ps1 := newTestPodSet("a", 2)
	ps2 := newTestPodSet("b", 5)
	root.AddPodSet(ps1)
	root.AddPodSet(ps2)
	clone := root.Clone()
	if clone == root {
		t.Fatal("clone should be a different pointer")
	}
	if clone.GetName() != "root" {
		t.Errorf("clone name mismatch: got %s", clone.GetName())
	}
	childPodSets := clone.GetChildPodSets()
	if len(childPodSets) != 2 {
		t.Fatalf("expected 2 podsets in clone, got %d", len(childPodSets))
	}
	names := []string{childPodSets[0].GetName(), childPodSets[1].GetName()}
	wantNames := []string{"a", "b"}
	if !reflect.DeepEqual(names, wantNames) && !reflect.DeepEqual(names, []string{"b", "a"}) {
		t.Errorf("podset names in clone mismatch: got %v want %v", names, wantNames)
	}
}

func TestClone_WithSubGroupsAndPodSets(t *testing.T) {
	root := NewSubGroupSet("root", nil)
	groupA := NewSubGroupSet("A", nil)
	groupB := NewSubGroupSet("B", nil)
	podSet := newTestPodSet("x", 1)
	groupA.AddPodSet(podSet)
	root.AddSubGroup(groupA)
	root.AddSubGroup(groupB)
	clone := root.Clone()
	if clone == root {
		t.Fatal("clone must not be same pointer as original")
	}
	if clone.GetName() != "root" {
		t.Errorf("clone name mismatch: got %s", clone.GetName())
	}
	subgroups := clone.GetChildGroups()
	if len(subgroups) != 2 {
		t.Fatalf("expected 2 subgroups in clone, got %d", len(subgroups))
	}
	found := false
	for _, g := range subgroups {
		if g.GetName() == "A" {
			cps := g.GetChildPodSets()
			if len(cps) != 1 || cps[0].GetName() != "x" {
				t.Errorf("cloned group A's podsets incorrect")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("cloned group A not found in subgroups")
	}
}

func TestGetPodSets(t *testing.T) {
	tests := []struct {
		name  string
		build func() (*SubGroupSet, map[string]*PodSet)
	}{
		{
			name: "no podSet",
			build: func() (*SubGroupSet, map[string]*PodSet) {
				sg := NewSubGroupSet("g1", nil)
				return sg, map[string]*PodSet{}
			},
		},
		{
			name: "single direct podSet",
			build: func() (*SubGroupSet, map[string]*PodSet) {
				sg := NewSubGroupSet("g1", nil)
				ps := newTestPodSet("ps1", 1)
				sg.AddPodSet(ps)
				return sg, map[string]*PodSet{"ps1": ps}
			},
		},
		{
			name: "single nested podSet",
			build: func() (*SubGroupSet, map[string]*PodSet) {
				sg := NewSubGroupSet("g1", nil)
				child := NewSubGroupSet("child", nil)
				ps := newTestPodSet("ps2", 2)
				child.AddPodSet(ps)
				sg.AddSubGroup(child)
				return sg, map[string]*PodSet{"ps2": ps}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg, want := tt.build()
			got := sg.GetAllPodSets()
			if len(got) != len(want) {
				t.Errorf("expected %d podsets, got %d", len(want), len(got))
			}
			for name, exp := range want {
				if got[name] != exp {
					t.Errorf("for podset %q: expected %v, got %v", name, exp, got[name])
				}
			}
		})
	}
}
