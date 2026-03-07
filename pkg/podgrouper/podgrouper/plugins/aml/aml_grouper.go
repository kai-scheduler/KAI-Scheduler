// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package aml

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

type AmlGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewAmlGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *AmlGrouper {
	return &AmlGrouper{
		defaultGrouper,
	}
}

func (amlGrouper *AmlGrouper) Name() string {
	return "AML Grouper"
}

// +kubebuilder:rbac:groups=amlarc.azureml.com,resources=amljobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=amlarc.azureml.com,resources=amljobs/finalizers,verbs=patch;update;create

func (amlGrouper *AmlGrouper) GetPodGroupMetadata(
	amlJob *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := amlGrouper.DefaultGrouper.GetPodGroupMetadata(amlJob, pod)
	if err != nil {
		return nil, err
	}

	replicas, err := getAmlJobReplicas(amlJob)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.MinAvailable = replicas

	return podGroupMetadata, nil
}

func getAmlJobReplicas(amlJob *unstructured.Unstructured) (int32, error) {
	amlJobReplicas, found, err :=
		unstructured.NestedInt64(amlJob.Object, "spec", "job", "options", "envs", "AZUREML_NODE_COUNT")
	if !found || err != nil {
		return 0, fmt.Errorf("azureml envs count not found in spec")
	}
	return int32(amlJobReplicas), nil
}
