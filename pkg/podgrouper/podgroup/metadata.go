// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"maps"
	"slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TopologyConstraintMetadata struct {
	PreferredTopologyLevel string
	RequiredTopologyLevel  string
	Topology               string
}

func (t *TopologyConstraintMetadata) DeepCopy() *TopologyConstraintMetadata {
	if t == nil {
		return nil
	}
	out := *t
	return &out
}

type SubGroupMetadata struct {
	Name                string
	MinAvailable        int32
	Parent              *string
	PodsReferences      []string
	TopologyConstraints *TopologyConstraintMetadata
}

func (s *SubGroupMetadata) DeepCopy() *SubGroupMetadata {
	if s == nil {
		return nil
	}
	out := *s
	if s.Parent != nil {
		p := *s.Parent
		out.Parent = &p
	}
	out.PodsReferences = slices.Clone(s.PodsReferences)
	out.TopologyConstraints = s.TopologyConstraints.DeepCopy()
	return &out
}

type Metadata struct {
	Annotations       map[string]string
	Labels            map[string]string
	PriorityClassName string
	Preemptibility    v2alpha2.Preemptibility
	Queue             string
	Namespace         string
	Name              string
	MinAvailable      int32
	Owner             metav1.OwnerReference
	SubGroups         []*SubGroupMetadata

	PreferredTopologyLevel string
	RequiredTopologyLevel  string
	Topology               string
}

// DeepCopy returns a deep copy of m. All reference-typed fields (maps, slices,
// pointers, and embedded types containing them — e.g. metav1.OwnerReference's
// *bool fields) are cloned so that mutating the returned value cannot alias
// the receiver.
//
// When adding a new field to Metadata, this method must clone any reference
// type. The reflection-based test in metadata_test.go enforces this by
// populating every reachable reference-typed field and asserting pointer
// distinctness, so a missed clone fails the test rather than aliasing in prod.
func (m *Metadata) DeepCopy() *Metadata {
	if m == nil {
		return nil
	}
	out := *m
	out.Annotations = maps.Clone(m.Annotations)
	out.Labels = maps.Clone(m.Labels)
	out.Owner = *m.Owner.DeepCopy()
	if m.SubGroups != nil {
		out.SubGroups = make([]*SubGroupMetadata, len(m.SubGroups))
		for i, sg := range m.SubGroups {
			out.SubGroups[i] = sg.DeepCopy()
		}
	}
	return &out
}

func (m *Metadata) FindSubGroupForPod(podNamespace, podName string) *SubGroupMetadata {
	if m.Namespace != podNamespace {
		return nil
	}
	for _, subGroup := range m.SubGroups {
		for _, podRef := range subGroup.PodsReferences {
			if podRef == podName {
				return subGroup
			}
		}
	}
	return nil
}
