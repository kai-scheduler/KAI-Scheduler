// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TopologyConstraintMetadata struct {
	PreferredTopologyLevel string
	RequiredTopologyLevel  string
	Topology               string
}

type SubGroupMetadata struct {
	Name                string
	MinAvailable        int32
	MinSubGroup         *int32
	Parent              *string
	PodsReferences      []string
	TopologyConstraints *TopologyConstraintMetadata
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
	MinSubGroup       *int32
	Owner             metav1.OwnerReference
	SubGroups         []*SubGroupMetadata

	PreferredTopologyLevel string
	RequiredTopologyLevel  string
	Topology               string

	// Warnings collects non-blocking messages surfaced as Warning events on the pod by the controller.
	// It is an internal field, not part of any public API surface.
	Warnings []string
}

// semiPreemptibleSegmentedWarning is emitted when a workload requests both automated segmentation and
// semi-preemptible. The two are mutually exclusive: a fully-gang auto-segmented tree has no surplus, so
// semi-preemptible is inert and the workload behaves as non-preemptible.
const semiPreemptibleSegmentedWarning = "semi-preemptible is not compatible with automatic segmentation " +
	"(kai.scheduler/segment-size): the segmented tree is fully gang and has no elastic surplus, so the " +
	"workload will behave as non-preemptible"

// WarnIfSemiPreemptibleSegmented appends a warning when a segmented workload is also semi-preemptible.
func (m *Metadata) WarnIfSemiPreemptibleSegmented(segmented bool) {
	if segmented && m.Preemptibility == v2alpha2.SemiPreemptible {
		m.Warnings = append(m.Warnings, semiPreemptibleSegmentedWarning)
	}
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
