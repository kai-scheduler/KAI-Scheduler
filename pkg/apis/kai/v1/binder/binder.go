// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package binder

import (
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	imageName                           = "binder"
	defaultResourceReservationImageName = "resource-reservation"
)

type Binder struct {
	Service *common.Service `json:"service,omitempty"`

	// ResourceReservation controls configuration for the resource reservation functionality
	// +kubebuilder:validation:Optional
	ResourceReservation *ResourceReservation `json:"resourceReservation,omitempty"`

	// Replicas specifies the number of replicas of the KAI binder service
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// MaxConcurrentReconciles is the maximum number of concurrent reconciles for both pods and BindRequests
	// +kubebuilder:validation:Optional
	MaxConcurrentReconciles *int `json:"maxConcurrentReconciles,omitempty"`

	// VolumeBindingTimeoutSeconds specifies the timeout for volume binding in seconds
	// +kubebuilder:validation:Optional
	VolumeBindingTimeoutSeconds *int `json:"volumeBindingTimeoutSeconds,omitempty"`

	// ProbePort specifies the health check port
	ProbePort *int `json:"probePort,omitempty"`

	// MetricsPort specifies the metrics service port
	MetricsPort *int `json:"metricsPort,omitempty"`

	// CDIEnabled Specifies if the gpu device plugin uses the cdi devices api to set gpu devices to the pods
	// leave empty if unsure to let the operator auto detect using ClusterPolicy (nvidia gpu-operator only)
	// +kubebuilder:validation:Optional
	CDIEnabled *bool `json:"cdiEnabled,omitempty"`
}

func (b *Binder) SetDefaultsWhereNeeded(replicaCount *int32) {
	b.Service = common.SetDefault(b.Service, &common.Service{})
	b.Service.Resources = common.SetDefault(b.Service.Resources, &common.Resources{})
	if b.Service.Resources.Requests == nil {
		b.Service.Resources.Requests = v1.ResourceList{}
	}
	if b.Service.Resources.Limits == nil {
		b.Service.Resources.Limits = v1.ResourceList{}
	}

	if _, found := b.Service.Resources.Requests[v1.ResourceCPU]; !found {
		b.Service.Resources.Requests[v1.ResourceCPU] = resource.MustParse("50m")
	}
	if _, found := b.Service.Resources.Requests[v1.ResourceMemory]; !found {
		b.Service.Resources.Requests[v1.ResourceMemory] = resource.MustParse("200Mi")
	}
	if _, found := b.Service.Resources.Limits[v1.ResourceCPU]; !found {
		b.Service.Resources.Limits[v1.ResourceCPU] = resource.MustParse("100m")
	}
	if _, found := b.Service.Resources.Limits[v1.ResourceMemory]; !found {
		b.Service.Resources.Limits[v1.ResourceMemory] = resource.MustParse("200Mi")
	}

	b.Service.SetDefaultsWhereNeeded(imageName)

	b.Replicas = common.SetDefault(b.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))

	b.ResourceReservation = common.SetDefault(b.ResourceReservation, &ResourceReservation{})
	b.ResourceReservation.SetDefaultsWhereNeeded()

	b.ProbePort = common.SetDefault(b.ProbePort, ptr.To(8081))
	b.MetricsPort = common.SetDefault(b.MetricsPort, ptr.To(8080))
}

type ResourceReservation struct {
	// Image is the image used by the resource reservation pods
	// +kubebuilder:validation:Optional
	Image *common.Image `json:"image,omitempty"`

	// AllocationTimeout specifies the timeout for resource reservation pod allocation in seconds
	// +kubebuilder:validation:Optional
	AllocationTimeout *int `json:"allocationTimeout,omitempty"`

	// Namespace is the name of the namespace where the resource reservation pods will run
	// +kubebuilder:validation:Optional
	Namespace *string `json:"namespace,omitempty"`

	// ServiceAccountName is the name of the service account that will be used by the resource reservation pods
	// +kubebuilder:validation:Optional
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`

	// AppLabel is the value that will be set for all resource reservation pods to the label `app`
	// +kubebuilder:validation:Optional
	AppLabel *string `json:"appLabel,omitempty"`

	// RuntimeClassName specifies the runtime class used by the reservation pods. Needs to allow access to the GPU
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// PodResources specifies the CPU and memory resource requests and limits for GPU reservation pods.
	// If not set, Kubernetes defaults will be used, which allows for better backward compatibility.
	// +kubebuilder:validation:Optional
	PodResources *common.Resources `json:"podResources,omitempty"`
}

func (r *ResourceReservation) SetDefaultsWhereNeeded() {
	r.Image = common.SetDefault(r.Image, &common.Image{})
	r.Image.Name = common.SetDefault(r.Image.Name, ptr.To(defaultResourceReservationImageName))
	r.Image.SetDefaultsWhereNeeded()

	r.Namespace = common.SetDefault(r.Namespace, ptr.To(constants.DefaultResourceReservationName))
	r.ServiceAccountName = common.SetDefault(r.ServiceAccountName, ptr.To(constants.DefaultResourceReservationName))
	r.AppLabel = common.SetDefault(r.AppLabel, ptr.To(constants.DefaultResourceReservationName))
	r.RuntimeClassName = common.SetDefault(r.RuntimeClassName, ptr.To(constants.DefaultRuntimeClassName))
}
