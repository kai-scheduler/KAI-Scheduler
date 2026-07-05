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

	// Warnings holds soft-validation messages raised while building the PodGroup. The controller
	// surfaces them as Warning events on the pod; they never block PodGroup creation.
	Warnings []string
}

// semiPreemptibleSegmentationWarning is raised when an automatically segmented workload is also
// semi-preemptible. The two are mutually exclusive: segmentation produces a fully-gang tree with no
// elastic surplus, so semi-preemptible is inert and the workload is scheduled as non-preemptible.
const semiPreemptibleSegmentationWarning = "PodGroup is both semi-preemptible and automatically segmented; " +
	"these are mutually exclusive. Semi-preemptible has no effect on a segmented (fully-gang) workload, " +
	"which will be scheduled as non-preemptible."

// WarnIfSemiPreemptibleSegmented records the semi-preemptible/segmentation conflict warning when the
// workload was auto-segmented and is also semi-preemptible.
func (m *Metadata) WarnIfSemiPreemptibleSegmented(segmented bool) {
	if segmented && m.Preemptibility == v2alpha2.SemiPreemptible {
		m.Warnings = append(m.Warnings, semiPreemptibleSegmentationWarning)
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
