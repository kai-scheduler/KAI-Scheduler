// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

func Test_createPodGroupForMetadata(t *testing.T) {
	tests := []struct {
		name     string
		input    Metadata
		expected *schedulingv2alpha2.PodGroup
	}{
		{
			name: "basic fields mapping",
			input: Metadata{
				Name:              "test-podgroup",
				Namespace:         "test-namespace",
				Labels:            map[string]string{"app": "test"},
				Annotations:       map[string]string{"annotation-key": "annotation-value"},
				MinAvailable:      5,
				Queue:             "default-queue",
				PriorityClassName: "high-priority",
				Preemptibility:    "preemptible",
				Owner: metav1.OwnerReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "test-deployment",
					UID:        "test-uid",
					Controller: ptr.To(true),
				},
				PreferredTopologyLevel: "rack",
				RequiredTopologyLevel:  "zone",
				Topology:               "custom-topology",
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-podgroup",
					Namespace:   "test-namespace",
					Labels:      map[string]string{"app": "test"},
					Annotations: map[string]string{"annotation-key": "annotation-value"},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
							Name:       "test-deployment",
							UID:        "test-uid",
							Controller: ptr.To(true),
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember:         ptr.To(int32(5)),
					Queue:             "default-queue",
					PriorityClassName: "high-priority",
					Preemptibility:    "preemptible",
					SubGroups:         []schedulingv2alpha2.SubGroup{},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{
						PreferredTopologyLevel: "rack",
						RequiredTopologyLevel:  "zone",
						Topology:               "custom-topology",
					},
				},
			},
		},
		{
			name: "empty subgroups",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 3,
				SubGroups:    []*SubGroupMetadata{},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "test-pod",
					UID:        "pod-uid",
				},
				PreferredTopologyLevel: "node",
				RequiredTopologyLevel:  "rack",
				Topology:               "test-topology",
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "test-pod",
							UID:        "pod-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(3)),
					SubGroups: []schedulingv2alpha2.SubGroup{},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{
						PreferredTopologyLevel: "node",
						RequiredTopologyLevel:  "rack",
						Topology:               "test-topology",
					},
				},
			},
		},
		{
			name: "subgroups without topology constraints",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 10,
				SubGroups: []*SubGroupMetadata{
					{
						Name:                "subgroup-1",
						MinAvailable:        5,
						PodsReferences:      []string{"pod-1"},
						TopologyConstraints: nil,
					},
					{
						Name:                "subgroup-2",
						MinAvailable:        5,
						PodsReferences:      []string{"pod-2"},
						TopologyConstraints: nil,
					},
				},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(10)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:               "subgroup-1",
							MinMember:          ptr.To(int32(5)),
							TopologyConstraint: nil,
						},
						{
							Name:               "subgroup-2",
							MinMember:          ptr.To(int32(5)),
							TopologyConstraint: nil,
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "subgroups with topology constraints",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 5,
				SubGroups: []*SubGroupMetadata{
					{
						Name:         "subgroup-1",
						MinAvailable: 5,
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "node",
							RequiredTopologyLevel:  "rack",
							Topology:               "topology-1",
						},
					},
				},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(5)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:      "subgroup-1",
							MinMember: ptr.To(int32(5)),
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "node",
								RequiredTopologyLevel:  "rack",
								Topology:               "topology-1",
							},
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "mixed subgroups - some with constraints, some without",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 15,
				SubGroups: []*SubGroupMetadata{
					{
						Name:         "subgroup-with-constraint",
						MinAvailable: 5,
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "socket",
							RequiredTopologyLevel:  "node",
							Topology:               "topology-a",
						},
					},
					{
						Name:                "subgroup-without-constraint",
						MinAvailable:        5,
						TopologyConstraints: nil,
					},
					{
						Name:         "another-with-constraint",
						MinAvailable: 5,
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "rack",
							RequiredTopologyLevel:  "zone",
							Topology:               "topology-b",
						},
					},
				},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(15)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:      "subgroup-with-constraint",
							MinMember: ptr.To(int32(5)),
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "socket",
								RequiredTopologyLevel:  "node",
								Topology:               "topology-a",
							},
						},
						{
							Name:               "subgroup-without-constraint",
							MinMember:          ptr.To(int32(5)),
							TopologyConstraint: nil,
						},
						{
							Name:      "another-with-constraint",
							MinMember: ptr.To(int32(5)),
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "rack",
								RequiredTopologyLevel:  "zone",
								Topology:               "topology-b",
							},
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "subgroups with parent references",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 10,
				SubGroups: []*SubGroupMetadata{
					{
						Name:         "parent-group",
						MinAvailable: 0,
						Parent:       nil,
					},
					{
						Name:         "child-subgroup-1",
						MinAvailable: 5,
						Parent:       ptr.To("parent-group"),
					},
					{
						Name:         "child-subgroup-2",
						MinAvailable: 5,
						Parent:       ptr.To("parent-group"),
					},
				},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(10)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:               "parent-group",
							MinMember:          ptr.To(int32(0)),
							Parent:             nil,
							TopologyConstraint: nil,
						},
						{
							Name:               "child-subgroup-1",
							MinMember:          ptr.To(int32(5)),
							Parent:             ptr.To("parent-group"),
							TopologyConstraint: nil,
						},
						{
							Name:               "child-subgroup-2",
							MinMember:          ptr.To(int32(5)),
							Parent:             ptr.To("parent-group"),
							TopologyConstraint: nil,
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "subgroups without parent references",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 10,
				SubGroups: []*SubGroupMetadata{
					{
						Name:         "subgroup-1",
						MinAvailable: 5,
						Parent:       nil,
					},
					{
						Name:         "subgroup-2",
						MinAvailable: 5,
						Parent:       nil,
					},
				},
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(10)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:               "subgroup-1",
							MinMember:          ptr.To(int32(5)),
							Parent:             nil,
							TopologyConstraint: nil,
						},
						{
							Name:               "subgroup-2",
							MinMember:          ptr.To(int32(5)),
							Parent:             nil,
							TopologyConstraint: nil,
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "podgroup level topology constraint",
			input: Metadata{
				Name:                   "test-podgroup",
				Namespace:              "test-namespace",
				MinAvailable:           5,
				PreferredTopologyLevel: "rack",
				RequiredTopologyLevel:  "zone",
				Topology:               "custom-topology",
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(5)),
					SubGroups: []schedulingv2alpha2.SubGroup{},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{
						PreferredTopologyLevel: "rack",
						RequiredTopologyLevel:  "zone",
						Topology:               "custom-topology",
					},
				},
			},
		},
		{
			name: "empty topology values",
			input: Metadata{
				Name:                   "test-podgroup",
				Namespace:              "test-namespace",
				MinAvailable:           5,
				PreferredTopologyLevel: "",
				RequiredTopologyLevel:  "",
				Topology:               "",
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(5)),
					SubGroups: []schedulingv2alpha2.SubGroup{},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{
						PreferredTopologyLevel: "",
						RequiredTopologyLevel:  "",
						Topology:               "",
					},
				},
			},
		},
		{
			name: "nil labels and annotations",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				Labels:       nil,
				Annotations:  nil,
				MinAvailable: 5,
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-podgroup",
					Namespace:   "test-namespace",
					Labels:      nil,
					Annotations: nil,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember:          ptr.To(int32(5)),
					SubGroups:          []schedulingv2alpha2.SubGroup{},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{},
				},
			},
		},
		{
			name: "complex hierarchical subgroups",
			input: Metadata{
				Name:         "test-podgroup",
				Namespace:    "test-namespace",
				MinAvailable: 20,
				SubGroups: []*SubGroupMetadata{
					{
						Name:         "parent-group",
						MinAvailable: 0,
						Parent:       nil,
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "rack",
							RequiredTopologyLevel:  "zone",
							Topology:               "topology-parent",
						},
					},
					{
						Name:         "child-1-1",
						MinAvailable: 5,
						Parent:       ptr.To("parent-group"),
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "node",
							RequiredTopologyLevel:  "rack",
							Topology:               "topology-child-1",
						},
					},
					{
						Name:                "child-1-2",
						MinAvailable:        5,
						Parent:              ptr.To("parent-group"),
						TopologyConstraints: nil,
					},
					{
						Name:         "orphan-group",
						MinAvailable: 10,
						Parent:       nil,
						TopologyConstraints: &TopologyConstraintMetadata{
							PreferredTopologyLevel: "socket",
							RequiredTopologyLevel:  "node",
							Topology:               "topology-orphan",
						},
					},
				},
				PreferredTopologyLevel: "cluster",
				RequiredTopologyLevel:  "datacenter",
				Topology:               "global-topology",
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "owner",
					UID:        "owner-uid",
				},
			},
			expected: &schedulingv2alpha2.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-podgroup",
					Namespace: "test-namespace",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "owner",
							UID:        "owner-uid",
						},
					},
				},
				Spec: schedulingv2alpha2.PodGroupSpec{
					MinMember: ptr.To(int32(20)),
					SubGroups: []schedulingv2alpha2.SubGroup{
						{
							Name:      "parent-group",
							MinMember: ptr.To(int32(0)),
							Parent:    nil,
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "rack",
								RequiredTopologyLevel:  "zone",
								Topology:               "topology-parent",
							},
						},
						{
							Name:      "child-1-1",
							MinMember: ptr.To(int32(5)),
							Parent:    ptr.To("parent-group"),
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "node",
								RequiredTopologyLevel:  "rack",
								Topology:               "topology-child-1",
							},
						},
						{
							Name:               "child-1-2",
							MinMember:          ptr.To(int32(5)),
							Parent:             ptr.To("parent-group"),
							TopologyConstraint: nil,
						},
						{
							Name:      "orphan-group",
							MinMember: ptr.To(int32(10)),
							Parent:    nil,
							TopologyConstraint: &schedulingv2alpha2.TopologyConstraint{
								PreferredTopologyLevel: "socket",
								RequiredTopologyLevel:  "node",
								Topology:               "topology-orphan",
							},
						},
					},
					TopologyConstraint: schedulingv2alpha2.TopologyConstraint{
						PreferredTopologyLevel: "cluster",
						RequiredTopologyLevel:  "datacenter",
						Topology:               "global-topology",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &Handler{}
			result := handler.createPodGroupForMetadata(tt.input)

			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("createPodGroupForMetadata() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
