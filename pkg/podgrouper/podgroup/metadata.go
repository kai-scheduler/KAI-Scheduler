// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
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
	Owner             metav1.OwnerReference
	SubGroups         []*SubGroupMetadata

	PreferredTopologyLevel string
	RequiredTopologyLevel  string
	Topology               string
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
