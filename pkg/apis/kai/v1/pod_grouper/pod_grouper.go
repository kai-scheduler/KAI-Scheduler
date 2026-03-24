// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package pod_grouper

import (
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	imageName = "podgrouper"
)

type PodGrouper struct {
	Service *common.Service `json:"service,omitempty"`

	// Args specifies the CLI arguments for the pod-grouper
	// +kubebuilder:validation:Optional
	Args *Args `json:"args,omitempty"`

	// ClientConfig specifies the configuration of k8s client
	// +kubebuilder:validation:Optional
	K8sClientConfig *common.K8sClientConfig `json:"k8sClientConfig,omitempty"`

	//MaxConcurrentReconciles specifies the number of max concurrent reconcile workers
	MaxConcurrentReconciles *int `json:"maxConcurrentReconciles,omitempty"`

	// Replicas specifies the number of replicas of the pod-grouper controller
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the pod-grouper
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

// Args defines command line arguments for the pod-grouper
type Args struct {
	// GangScheduleKnative specifies whether to enable gang scheduling for Knative revisions. Default is true. Disable to allow multiple nodepools per revision.
	// +kubebuilder:validation:Optional
	GangScheduleKnative *bool `json:"gangScheduleKnative,omitempty"`

	// DefaultPrioritiesConfigMapName The name of the configmap that contains default priorities for pod groups
	// +kubebuilder:validation:Optional
	DefaultPrioritiesConfigMapName *string `json:"defaultPrioritiesConfigMapName,omitempty"`

	// DefaultPrioritiesConfigMapNamespace The namespace of the configmap that contains default priorities for pod groups
	// +kubebuilder:validation:Optional
	DefaultPrioritiesConfigMapNamespace *string `json:"defaultPrioritiesConfigMapNamespace,omitempty"`
}

func (pg *PodGrouper) SetDefaultsWhereNeeded(replicaCount *int32, globalVPA *common.VPASpec) {
	pg.Service = common.SetDefault(pg.Service, &common.Service{})
	pg.Service.SetDefaultsWhereNeeded(imageName)

	if _, found := pg.Service.Resources.Requests[v1.ResourceCPU]; !found {
		pg.Service.Resources.Requests[v1.ResourceCPU] = resource.MustParse("50m")
	}
	if _, found := pg.Service.Resources.Requests[v1.ResourceMemory]; !found {
		pg.Service.Resources.Requests[v1.ResourceMemory] = resource.MustParse("200Mi")
	}
	if _, found := pg.Service.Resources.Limits[v1.ResourceCPU]; !found {
		pg.Service.Resources.Limits[v1.ResourceCPU] = resource.MustParse("100m")
	}
	if _, found := pg.Service.Resources.Limits[v1.ResourceMemory]; !found {
		pg.Service.Resources.Limits[v1.ResourceMemory] = resource.MustParse("200Mi")
	}

	pg.Args = common.SetDefault(pg.Args, &Args{})
	pg.Replicas = common.SetDefault(pg.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))
	pg.K8sClientConfig = common.SetDefault(pg.K8sClientConfig, &common.K8sClientConfig{})

	if pg.VPA == nil {
		pg.VPA = globalVPA
	}
}
