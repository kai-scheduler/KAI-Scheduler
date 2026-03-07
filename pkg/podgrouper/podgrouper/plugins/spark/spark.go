// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package spark

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	sparkAppLabelName         = "spark-app-name"
	sparkAppSelectorLabelName = "spark-app-selector"
)

type SparkGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewSparkGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *SparkGrouper {
	return &SparkGrouper{
		defaultGrouper,
	}
}

func (sg *SparkGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod) (*podgroup.Metadata, error) {
	podGroupMetadata, err := sg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	podGroupMetadata.Name = pod.Labels[sparkAppSelectorLabelName]

	return podGroupMetadata, nil
}

func IsSparkPod(pod *v1.Pod) bool {
	_, foundSparkApp := pod.Labels[sparkAppLabelName]
	_, foundSparkAppSelector := pod.Labels[sparkAppSelectorLabelName]
	return foundSparkApp && foundSparkAppSelector
}
