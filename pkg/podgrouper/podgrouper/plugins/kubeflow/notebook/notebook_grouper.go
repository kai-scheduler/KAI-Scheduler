// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package notebook

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

type NotebookGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewNotebookGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *NotebookGrouper {
	return &NotebookGrouper{
		DefaultGrouper: defaultGrouper,
	}
}

func (ng *NotebookGrouper) Name() string {
	return "Kubeflow Notebook Grouper"
}

func (ng *NotebookGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := ng.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}
	metadata.PriorityClassName = ng.CalcPodGroupPriorityClass(topOwner, pod, constants.BuildPriorityClass)
	return metadata, nil
}
