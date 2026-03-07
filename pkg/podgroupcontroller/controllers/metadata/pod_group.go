// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgroupcontroller/controllers/resources"
)

type PodGroupMetadata struct {
	Preemptible bool

	// Current allocated GPU (in fracions), CPU (in millicpus), Memory in megabytes and any extra resources in ints
	// for all resources used by pods of this pod group
	Allocated v1.ResourceList `json:"allocated,omitempty"`

	// Current requested GPU (in fracions), CPU (in millicpus) and Memory in megabytes any extra resources in ints
	// for all resources used or requested by pods of this pod group
	Requested v1.ResourceList `json:"requested,omitempty"`
}

func NewPodGroupMetadata() *PodGroupMetadata {
	return &PodGroupMetadata{
		Allocated: v1.ResourceList{},
		Requested: v1.ResourceList{},
	}
}

func (pgm *PodGroupMetadata) AddPodMetadata(podMetadata *PodMetadata) {
	pgm.Requested = resources.SumResources(pgm.Requested, podMetadata.RequestedResources)
	pgm.Allocated = resources.SumResources(pgm.Allocated, podMetadata.AllocatedResources)
}
