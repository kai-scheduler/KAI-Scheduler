// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package kubeflow

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

type KubeflowDistributedGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewKubeflowDistributedGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *KubeflowDistributedGrouper {
	return &KubeflowDistributedGrouper{
		DefaultGrouper: defaultGrouper,
	}
}

// +kubebuilder:rbac:groups=kubeflow.org,resources=mpijobs;notebooks;pytorchjobs;jaxjobs;tfjobs;xgboostjobs;scheduledworkflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubeflow.org,resources=mpijobs/finalizers;notebooks/finalizers;pytorchjobs/finalizers;jaxjobs/finalizers;tfjobs/finalizers;xgboostjobs/finalizers;scheduledworkflows/finalizers,verbs=patch;update;create

func (kdg *KubeflowDistributedGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod,
	replicaSpecName string, mandatorySpecNames []string) (*podgroup.Metadata, error) {
	podGroupMetadata, err := kdg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	minAvailable, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	if err != nil {
		return nil, err
	}
	if found {
		podGroupMetadata.MinAvailable = int32(minAvailable)
		return podGroupMetadata, nil
	}

	totalReplicas, err :=
		calcJobNumOfPods(topOwner, replicaSpecName, mandatorySpecNames)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.MinAvailable = int32(totalReplicas)

	return podGroupMetadata, nil
}

func (kdg *KubeflowDistributedGrouper) Name() string {
	return "Kubeflow Distributed Grouper"
}

func calcJobNumOfPods(
	topOwner *unstructured.Unstructured,
	replicaSpecName string, mandatorySpecNames []string,
) (totalReplicas int64, err error) {
	replicaSpecs, found, err := unstructured.NestedMap(topOwner.Object, "spec", replicaSpecName)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("the type of the job %s doesn't have a %s field. Please fix the replica field",
			topOwner.GetName(), replicaSpecName)
	}

	for _, name := range mandatorySpecNames {
		if _, found := replicaSpecs[name]; !found {
			return 0, fmt.Errorf("the type of the job %s doesn't have a %s replica spec which is required. Please fix the replica field",
				topOwner.GetName(), name)
		}
	}

	for specName := range replicaSpecs {
		replicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", replicaSpecName, specName, "replicas")
		if err != nil {
			return 0, err
		}
		if !found || replicas == 0 {
			return 0, fmt.Errorf("the type of the job %s doesn't allow for 0 %s pods. Please fix the replica field",
				topOwner.GetName(), specName)
		}
		totalReplicas += replicas
	}

	if totalReplicas <= 0 {
		return 0, fmt.Errorf(
			"failed calculate number of pods for a distributed job %s. Must be greater than 0, but got %v",
			topOwner.GetName(), totalReplicas)
	}

	return totalReplicas, nil
}
