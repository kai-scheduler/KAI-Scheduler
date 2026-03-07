// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tensorflow

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	ReplicaSpecName = "tfReplicaSpecs"
)

type TensorFlowGrouper struct {
	*kubeflow.KubeflowDistributedGrouper
}

func NewTensorFlowGrouper(kubeflowGrouper *kubeflow.KubeflowDistributedGrouper) *TensorFlowGrouper {
	return &TensorFlowGrouper{
		kubeflowGrouper,
	}
}

func (tfg *TensorFlowGrouper) Name() string {
	return "TensorFlow Grouper"
}

func (tfg *TensorFlowGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	return tfg.KubeflowDistributedGrouper.GetPodGroupMetadata(topOwner, pod, ReplicaSpecName, []string{})
}
