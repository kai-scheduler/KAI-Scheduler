// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
)

type RayClusterGrouper struct {
	*RayGrouper
}

func NewRayClusterGrouper(rayGrouper *RayGrouper) *RayClusterGrouper {
	return &RayClusterGrouper{
		RayGrouper: rayGrouper,
	}
}

func (rcg *RayClusterGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	return rcg.getPodGroupMetadataWithClusterNamePath(topOwner, pod, nil)
}
