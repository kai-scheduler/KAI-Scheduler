// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	ReplicaSpecName = "pytorchReplicaSpecs"
)

type PyTorchGrouper struct {
	*kubeflow.KubeflowDistributedGrouper
}

func NewPyTorchGrouper(kubeflowGrouper *kubeflow.KubeflowDistributedGrouper) *PyTorchGrouper {
	return &PyTorchGrouper{
		KubeflowDistributedGrouper: kubeflowGrouper,
	}
}

func (ptg *PyTorchGrouper) Name() string {
	return "PyTorchJob Grouper"
}

func (ptg *PyTorchGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := ptg.KubeflowDistributedGrouper.GetPodGroupMetadata(topOwner, pod, ReplicaSpecName, []string{})
	if err != nil {
		return nil, err
	}

	minReplicas, err := getMinReplicas(topOwner)
	if err == nil {
		podGroupMetadata.MinAvailable = int32(minReplicas)
	}

	minAvailable, err := getMinAvailable(topOwner)
	if err == nil {
		podGroupMetadata.MinAvailable = int32(minAvailable)
	}

	return podGroupMetadata, nil
}

func getMinReplicas(topOwner *unstructured.Unstructured) (int64, error) {
	minReplicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "elasticPolicy", "minReplicas")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("minReplicas not found in PyTorchJob %s/%s", topOwner.GetNamespace(), topOwner.GetName())
	}
	return minReplicas, nil
}

func getMinAvailable(topOwner *unstructured.Unstructured) (int64, error) {
	minReplicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("minAvailable not found in PyTorchJob %s/%s", topOwner.GetNamespace(), topOwner.GetName())
	}
	return minReplicas, nil
}
