// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
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
	return rjg.getPodGroupMetadataWithClusterNamePath(topOwner, pod, [][]string{{"status", "rayClusterName"}})
}
