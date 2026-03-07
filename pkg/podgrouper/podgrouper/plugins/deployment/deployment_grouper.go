// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployment

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

type DeploymentGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewDeploymentGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *DeploymentGrouper {
	return &DeploymentGrouper{
		defaultGrouper,
	}
}

func (dg *DeploymentGrouper) Name() string {
	return "Deployment Grouper"
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=patch;update;create

func (dg *DeploymentGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := dg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}
	metadata.Owner = metav1.OwnerReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.GetName(),
		UID:        pod.GetUID(),
	}
	metadata.PriorityClassName = dg.CalcPodGroupPriorityClass(topOwner, pod, constants.InferencePriorityClass)

	metadata.Name = fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, pod.GetName(), pod.GetUID())

	return metadata, nil
}
