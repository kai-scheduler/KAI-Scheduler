// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
)

type RayJobGrouper struct {
	*RayGrouper
}

func NewRayJobGrouper(rayGrouper *RayGrouper) *RayJobGrouper {
	return &RayJobGrouper{
		RayGrouper: rayGrouper,
	}
}

func (rjg *RayJobGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := rjg.getPodGroupMetadataWithClusterNamePath(topOwner, pod, [][]string{{"status", "rayClusterName"}})
	if err != nil {
		return nil, err
	}
	// RayJobs support spec.suspend — use suspend-based preemption instead
	// of direct pod deletion. KubeRay v1.5+ handles suspend natively.
	if metadata.Annotations == nil {
		metadata.Annotations = map[string]string{}
	}
	metadata.Annotations["kai.scheduler/eviction-strategy"] = eviction_info.EvictionStrategySuspend
	return metadata, nil
}
