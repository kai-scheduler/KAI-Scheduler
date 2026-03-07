// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/topology_info"
)

type wantGroup struct {
	Name               string
	Groups             []*wantGroup
	PodSets            []*wantPodSet
	TopologyConstraint *topology_info.TopologyConstraintInfo
}

type wantPodSet struct {
	Name               string
	MinMember          int32
	TopologyConstraint *topology_info.TopologyConstraintInfo
}

func equalTopologyConstraint(got, want *topology_info.TopologyConstraintInfo) bool {
	if got == nil && want == nil {
		return true
	}
	if got == nil || want == nil {
		return false
	}

	return got.Topology == want.Topology &&
		got.RequiredLevel == want.RequiredLevel &&
		got.PreferredLevel == want.PreferredLevel
}

func checkGroupStructure(t *testing.T, got *SubGroupSet, want *wantGroup) {
	if got == nil {
		t.Fatalf("expected SubGroupSet %q, got nil", want.Name)
	}
	if got.GetName() != want.Name {
		t.Fatalf("expected SubGroupSet name %q, got %q", want.Name, got.GetName())
	}
	if !equalTopologyConstraint(got.GetTopologyConstraint(), want.TopologyConstraint) {
		t.Fatalf("expected topology constraint %q, got %q", want.TopologyConstraint, got.GetTopologyConstraint())
	}
	// Check PodSets
	if len(got.podSets) != len(want.PodSets) {
		t.Errorf("SubGroupSet %q: expected %d podSets, got %d", want.Name, len(want.PodSets), len(got.podSets))
	} else {
		used := make([]bool, len(got.podSets))
		for _, wp := range want.PodSets {
			found := false
			for i, gp := range got.podSets {
				if used[i] {
					continue
				}
				if gp.GetName() != wp.Name {
					continue
				}
				used[i] = true
				found = true

				if gp.GetMinAvailable() != wp.MinMember {
					t.Errorf("SubGroupSet %q: expected minMember %v got %v",
						wp.Name, wp.MinMember, gp.GetMinAvailable())
				}
				if !equalTopologyConstraint(gp.GetTopologyConstraint(), wp.TopologyConstraint) {
					t.Errorf("SubGroupSet %q: expected topology constraint %q got %q",
						wp.Name, wp.TopologyConstraint, gp.GetTopologyConstraint())
				}
			}
			if !found {
				t.Errorf("SubGroupSet %q: expected podSet %q not found", want.Name, wp.Name)
			}
		}
	}
	// Check children groups (SubGroupSets)
	if len(got.groups) != len(want.Groups) {
		t.Errorf("SubGroupSet %q: expected %d child groups, got %d", want.Name, len(want.Groups), len(got.groups))
		return
	}
	for _, wantChild := range want.Groups {
		found := false
		for _, gotChild := range got.groups {
			if gotChild.GetName() != wantChild.Name {
				continue
			}
			checkGroupStructure(t, gotChild, wantChild)
			found = true
			break
		}
		if !found {
			t.Errorf("SubGroupSet %q: expected group child %q not found among %v", want.Name, wantChild.Name, childrenNames(got.groups))
		}
	}
}

func childrenNames(groups []*SubGroupSet) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.GetName())
	}
	return names
}

func TestFromPodGroup_FullTree(t *testing.T) {
	tests := []struct {
		name     string
		podGroup *v2alpha2.PodGroup
		want     *wantGroup
		wantErr  string // substring match
	}{
		{
			name: "simple two-level",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "jobA"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "sg1", Parent: nil},
						{Name: "sg2", Parent: ptr.To("sg1"), MinMember: 3},
					},
				},
			},
			want: &wantGroup{
				Name: RootSubGroupSetName,
				Groups: []*wantGroup{
					{
						Name:    "sg1",
						Groups:  nil,
						PodSets: []*wantPodSet{{Name: "sg2", MinMember: 3}},
					},
				},
				PodSets: nil,
			},
		},
		{
			name: "hierarchy with one leaf",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns2", Name: "jobB"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "rootchild", Parent: nil},
						{Name: "middle", Parent: ptr.To("rootchild")},
						{Name: "leaf", Parent: ptr.To("middle"), MinMember: 5},
					},
				},
			},
			want: &wantGroup{
				Name: RootSubGroupSetName,
				Groups: []*wantGroup{
					{
						Name: "rootchild",
						Groups: []*wantGroup{
							{
								Name:    "middle",
								Groups:  nil,
								PodSets: []*wantPodSet{{Name: "leaf", MinMember: 5}},
							},
						},
						PodSets: nil,
					},
				},
				PodSets: nil,
			},
		},
		{
			name: "podgroup with global topology constraint",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns3", Name: "jobC"},
				Spec: v2alpha2.PodGroupSpec{
					TopologyConstraint: v2alpha2.TopologyConstraint{
						Topology:               "topology",
						PreferredTopologyLevel: "zone",
					},
					SubGroups: []v2alpha2.SubGroup{
						{
							Name:      "rootchild",
							Parent:    nil,
							MinMember: 3,
							TopologyConstraint: &v2alpha2.TopologyConstraint{
								Topology:              "topology",
								RequiredTopologyLevel: "rack",
							},
						},
					},
				},
			},
			want: &wantGroup{
				Name:   RootSubGroupSetName,
				Groups: nil,
				PodSets: []*wantPodSet{
					{
						Name: "rootchild",
						TopologyConstraint: &topology_info.TopologyConstraintInfo{
							Topology:      "topology",
							RequiredLevel: "rack",
						},
						MinMember: 3,
					},
				},
				TopologyConstraint: &topology_info.TopologyConstraintInfo{
					Topology:       "topology",
					PreferredLevel: "zone",
				},
			},
		},
		{
			name: "subgroups with topology constraints",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns4", Name: "jobD"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "rootchild", Parent: nil, TopologyConstraint: &v2alpha2.TopologyConstraint{
							Topology:               "topology",
							PreferredTopologyLevel: "zone",
						}},
						{Name: "leaf1", Parent: ptr.To("rootchild"), MinMember: 2, TopologyConstraint: &v2alpha2.TopologyConstraint{
							Topology:              "topology",
							RequiredTopologyLevel: "rack",
						}},
						{Name: "leaf2", Parent: ptr.To("rootchild"), MinMember: 3, TopologyConstraint: &v2alpha2.TopologyConstraint{
							Topology:              "topology",
							RequiredTopologyLevel: "host",
						}},
					},
				},
			},
			want: &wantGroup{
				Name: RootSubGroupSetName,
				Groups: []*wantGroup{
					{
						Name:   "rootchild",
						Groups: nil,
						PodSets: []*wantPodSet{
							{
								Name:      "leaf1",
								MinMember: 2,
								TopologyConstraint: &topology_info.TopologyConstraintInfo{
									Topology:      "topology",
									RequiredLevel: "rack",
								},
							},
							{
								Name:      "leaf2",
								MinMember: 3,
								TopologyConstraint: &topology_info.TopologyConstraintInfo{
									Topology:      "topology",
									RequiredLevel: "host",
								},
							},
						},
						TopologyConstraint: &topology_info.TopologyConstraintInfo{
							Topology:       "topology",
							PreferredLevel: "zone",
						},
					},
				},
				PodSets: nil,
			},
		},
		{
			name: "empty subgroups",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns5", Name: "empty"},
				Spec:       v2alpha2.PodGroupSpec{SubGroups: nil},
			},
			want: &wantGroup{
				Name:    RootSubGroupSetName,
				Groups:  nil,
				PodSets: nil,
			},
		},
		{
			name: "parent not found",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns6", Name: "bad"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "sg1", Parent: ptr.To("nonexistent"), MinMember: 2},
					},
				},
			},
			want:    nil,
			wantErr: "parent subgroup <nonexistent> of <sg1> not found",
		},
		{
			name: "subgroup set parent not found",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns7", Name: "bad"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "p1", Parent: nil},
						{Name: "c1", Parent: ptr.To("p1")},
						{Name: "c2", Parent: ptr.To("no_such_set"), MinMember: 1},
					},
				},
			},
			want:    nil,
			wantErr: "parent subgroup <no_such_set> of <c2> not found",
		},
		{
			name: "duplicate subgroup names",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns8", Name: "dup"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "sg", Parent: nil},
						{Name: "sg", Parent: ptr.To("sg"), MinMember: 1},
					},
				},
			},
			want:    nil,
			wantErr: "subgroup <sg> already exists",
		},
		{
			name: "parent of deep child not found",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns-missing", Name: "missingparent"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "root", Parent: nil},
						{Name: "c1", Parent: ptr.To("root")},
						{Name: "c2", Parent: ptr.To("doesnotexist"), MinMember: 4},
					},
				},
			},
			want:    nil,
			wantErr: "parent subgroup <doesnotexist> of <c2> not found",
		},
		{
			name: "intermediate subgroup parent not found",
			podGroup: &v2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns-intermediate", Name: "missingintermediate"},
				Spec: v2alpha2.PodGroupSpec{
					SubGroups: []v2alpha2.SubGroup{
						{Name: "parent", Parent: nil},
						{Name: "mid", Parent: ptr.To("no_such_parent")},
						{Name: "leaf", Parent: ptr.To("mid"), MinMember: 1},
					},
				},
			},
			want:    nil,
			wantErr: "parent subgroup <no_such_parent> of <mid> not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := FromPodGroup(tt.podGroup)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if got, want := err.Error(), tt.wantErr; !strings.Contains(got, want) {
					t.Errorf("expected error containing %q, got %q", want, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == nil && root != nil {
				t.Fatalf("expected no SubGroupSet, got one: %#v", root)
			}
			if tt.want != nil {
				checkGroupStructure(t, root, tt.want)
			}
		})
	}
}
