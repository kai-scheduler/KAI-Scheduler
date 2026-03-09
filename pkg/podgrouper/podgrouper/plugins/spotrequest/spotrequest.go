// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package spotrequest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type SpotRequestGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewSpotRequestGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *SpotRequestGrouper {
	return &SpotRequestGrouper{
		DefaultGrouper: defaultGrouper,
	}
}

// +kubebuilder:rbac:groups=egx.nvidia.io,resources=spotrequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=egx.nvidia.io,resources=spotrequests/finalizers,verbs=patch;update;create

func (srg *SpotRequestGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	pgMetadata, err := srg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	pgMetadata.PriorityClassName = srg.DefaultGrouper.CalcPodGroupPriorityClass(topOwner, pod, constants.InferencePriorityClass)

	return pgMetadata, nil

}
