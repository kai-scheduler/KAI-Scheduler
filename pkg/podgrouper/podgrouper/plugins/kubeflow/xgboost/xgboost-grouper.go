// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package xgboost

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	ReplicaSpecName = "xgbReplicaSpecs"
	MasterName      = "Master"
	WorkerName      = "Worker"
)

type XGBoostGrouper struct {
	*kubeflow.KubeflowDistributedGrouper
}

func NewXGBoostGrouper(kubeflowGrouper *kubeflow.KubeflowDistributedGrouper) *XGBoostGrouper {
	return &XGBoostGrouper{
		kubeflowGrouper,
	}
}

func (xgbg *XGBoostGrouper) Name() string {
	return "XGBoost Grouper"
}

func (xgbg *XGBoostGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	return xgbg.KubeflowDistributedGrouper.GetPodGroupMetadata(topOwner, pod, ReplicaSpecName, []string{MasterName, WorkerName})
}
