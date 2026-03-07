// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podjob

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/spark"
)

type PodJobGrouper struct {
	*spark.SparkGrouper
	*defaultgrouper.DefaultGrouper
}

func NewPodJobGrouper(defaultGrouper *defaultgrouper.DefaultGrouper, sparkGrouper *spark.SparkGrouper) *PodJobGrouper {
	return &PodJobGrouper{
		SparkGrouper:   sparkGrouper,
		DefaultGrouper: defaultGrouper,
	}
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update;create;patch

func (pjg *PodJobGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	if spark.IsSparkPod(pod) {
		return pjg.SparkGrouper.GetPodGroupMetadata(topOwner, pod)
	}
	return pjg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
}
