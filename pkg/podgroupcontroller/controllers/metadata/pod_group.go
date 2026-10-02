// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/resources"
)

type PodGroupMetadata struct {
	Preemptibility v2alpha2.Preemptibility

	// Current allocated GPU (in fracions), CPU (in millicpus), Memory in megabytes and any extra resources in ints
	// for all resources used by pods of this pod group
	Allocated v1.ResourceList `json:"allocated,omitempty"`

	// Same as Allocated, but restricted to the pods the scheduler published as core
	// (PodGroup.Status.SchedulingState.CorePods). Only meaningful for semi-preemptible pod groups.
	CoreAllocated v1.ResourceList `json:"coreAllocated,omitempty"`

	// Current requested GPU (in fracions), CPU (in millicpus) and Memory in megabytes any extra resources in ints
	// for all resources used or requested by pods of this pod group
	Requested v1.ResourceList `json:"requested,omitempty"`
}

func NewPodGroupMetadata() *PodGroupMetadata {
	return &PodGroupMetadata{
		Allocated:     v1.ResourceList{},
		CoreAllocated: v1.ResourceList{},
		Requested:     v1.ResourceList{},
	}
}

func (pgm *PodGroupMetadata) AddPodMetadata(podMetadata *PodMetadata, isCore bool) {
	pgm.Requested = resources.SumResources(pgm.Requested, podMetadata.RequestedResources)
	pgm.Allocated = resources.SumResources(pgm.Allocated, podMetadata.AllocatedResources)
	if isCore {
		pgm.CoreAllocated = resources.SumResources(pgm.CoreAllocated, podMetadata.AllocatedResources)
	}
}
