// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
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

// podSetWithRunningPods builds a PodSet with the given number of Running tasks (active allocated).
func podSetWithRunningPods(name string, minAvailable int32, runningCount int) *PodSet {
	ps := NewPodSet(name, minAvailable, nil)
	for i := range runningCount {
		ps.AssignTask(&pod_info.PodInfo{
			UID:    common_info.PodID(fmt.Sprintf("%s-%d", name, i)),
			Status: pod_status.Running,
		})
	}
	return ps
}

func TestNewSubGroupSet(t *testing.T) {
	sg := NewSubGroupSet("root", nil)
	if sg.GetName() != "root" {
		t.Errorf("expected name to be 'root', got %s", sg.GetName())
	}
	if len(sg.GetDirectSubgroupsSets()) != 0 {
		t.Errorf("expected 0 child groups, got %d", len(sg.GetDirectSubgroupsSets()))
	}
	if len(sg.GetDirectPodSets()) != 0 {
		t.Errorf("expected 0 podsets, got %d", len(sg.GetDirectPodSets()))
	}
}

func TestAddSubGroup(t *testing.T) {
	parent := NewSubGroupSet("parent", nil)
	child := NewSubGroupSet("child", nil)
	parent.AddSubGroup(child)
	groups := parent.GetDirectSubgroupsSets()
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
	ps := parent.GetDirectPodSets()
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
	childPodSets := clone.GetDirectPodSets()
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
	subgroups := clone.GetDirectSubgroupsSets()
	if len(subgroups) != 2 {
		t.Fatalf("expected 2 subgroups in clone, got %d", len(subgroups))
	}
	found := false
	for _, g := range subgroups {
		if g.GetName() == "A" {
			cps := g.GetDirectPodSets()
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
			got := sg.GetDescendantPodSets()
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

func TestGetMinChildrenToSatisfy(t *testing.T) {
	t.Run("no_minSubGroup_empty", func(t *testing.T) {
		sg := NewSubGroupSet("root", nil)
		if got := sg.GetMinMembersToSatisfy(); got != 0 {
			t.Errorf("GetMinChildrenToSatisfy() = %d, want 0", got)
		}
	})
	t.Run("no_minSubGroup_counts_children", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddSubGroup(NewSubGroupSet("a", nil))
		root.AddSubGroup(NewSubGroupSet("b", nil))
		root.AddPodSet(newTestPodSet("ps", 1))
		if got := root.GetMinMembersToSatisfy(); got != 3 {
			t.Errorf("GetMinChildrenToSatisfy() = %d, want 3", got)
		}
	})
	t.Run("minSubGroup_overrides_count", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddSubGroup(NewSubGroupSet("a", nil))
		root.AddSubGroup(NewSubGroupSet("b", nil))
		min := int32(1)
		root.SetMinSubGroup(&min)
		if got := root.GetMinMembersToSatisfy(); got != 1 {
			t.Errorf("GetMinChildrenToSatisfy() = %d, want 1", got)
		}
	})
}

func TestGetNumActiveAllocatedDirectSubGroups(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		sg := NewSubGroupSet("root", nil)
		if got := sg.GetNumActiveAllocatedDirectSubGroups(); got != 0 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 0", got)
		}
	})
	t.Run("podSet_not_satisfied", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddPodSet(podSetWithRunningPods("ps", 3, 2))
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 0 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 0", got)
		}
	})
	t.Run("podSet_satisfied", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddPodSet(podSetWithRunningPods("ps", 3, 3))
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 1 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 1", got)
		}
	})
	t.Run("mixed_podSets", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddPodSet(podSetWithRunningPods("ok", 1, 1))
		root.AddPodSet(podSetWithRunningPods("bad", 2, 1))
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 1 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 1", got)
		}
	})
	t.Run("nested_child_satisfied_by_podSet", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		child := NewSubGroupSet("child", nil)
		child.AddPodSet(podSetWithRunningPods("ps", 1, 1))
		root.AddSubGroup(child)
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 1 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 1", got)
		}
	})
	t.Run("empty_child_subgroup_counts_satisfied", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		root.AddSubGroup(NewSubGroupSet("emptyChild", nil))
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 1 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 1 (0>=0 for min children)", got)
		}
	})
	t.Run("nested_child_not_satisfied_until_pod_min_met", func(t *testing.T) {
		root := NewSubGroupSet("root", nil)
		child := NewSubGroupSet("child", nil)
		ps := podSetWithRunningPods("ps", 2, 1)
		child.AddPodSet(ps)
		root.AddSubGroup(child)
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 0 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 0", got)
		}
		ps.AssignTask(&pod_info.PodInfo{UID: common_info.PodID("ps-extra"), Status: pod_status.Running})
		if got := root.GetNumActiveAllocatedDirectSubGroups(); got != 1 {
			t.Errorf("GetNumActiveAllocatedDirectSubGroups() = %d, want 1", got)
		}
	})
}
