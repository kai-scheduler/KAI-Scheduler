// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SubGroupMetadata struct {
	Name           string
	MinAvailable   int32
	Parent         *string
	PodsReferences []string
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
