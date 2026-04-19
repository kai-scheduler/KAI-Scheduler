// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package pod_group_controller

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

const (
	imageName = "podgroupcontroller"
)

type PodGroupController struct {
	Service *common.Service `json:"service,omitempty"`

	// MaxConcurrentReconciles specifies the number of max concurrent reconcile workers
	// +kubebuilder:validation:Optional
	MaxConcurrentReconciles *int `json:"maxConcurrentReconciles,omitempty"`

	// Replicas specifies the number of replicas of the pod-group controller
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`
}

func (pg *PodGroupController) SetDefaultsWhereNeeded(replicaCount *int32) {
	pg.Service = common.SetDefault(pg.Service, &common.Service{})
	pg.Service.SetDefaultsWhereNeeded(imageName)

	if _, found := pg.Service.Resources.Requests[v1.ResourceCPU]; !found {
		pg.Service.Resources.Requests[v1.ResourceCPU] = resource.MustParse("20m")
	}
	if _, found := pg.Service.Resources.Requests[v1.ResourceMemory]; !found {
		pg.Service.Resources.Requests[v1.ResourceMemory] = resource.MustParse("100Mi")
	}
	if _, found := pg.Service.Resources.Limits[v1.ResourceCPU]; !found {
		pg.Service.Resources.Limits[v1.ResourceCPU] = resource.MustParse("500m")
	}
	if _, found := pg.Service.Resources.Limits[v1.ResourceMemory]; !found {
		pg.Service.Resources.Limits[v1.ResourceMemory] = resource.MustParse("100Mi")
	}

	pg.Replicas = common.SetDefault(pg.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))
}
