// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package binder

import (
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const (
	imageName                           = "binder"
	defaultResourceReservationImageName = "resource-reservation"

	VolumeBindingPluginName    = "volumebinding"
	DynamicResourcesPluginName = "dynamicresources"
	GPUSharingPluginName       = "gpusharing"

	BindTimeoutSecondsArgument = "bindTimeoutSeconds"
	CDIEnabledArgument         = "cdiEnabled"

	DefaultBindTimeoutSeconds = 120
	DefaultCDIEnabled         = false
)

var defaultPluginPriorities = map[string]int{
	VolumeBindingPluginName:    300,
	DynamicResourcesPluginName: 200,
	GPUSharingPluginName:       100,
}

// PluginConfig allows overriding binder plugin settings.
type PluginConfig struct {
	// Enabled controls whether this plugin is active. Defaults to true.
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Priority controls the ordering of this plugin. Higher values run first.
	// +kubebuilder:validation:Optional
	Priority *int `json:"priority,omitempty"`

	// Arguments are key-value pairs passed to the plugin. When specified, they replace
	// the default arguments for the plugin.
	// +kubebuilder:validation:Optional
	Arguments map[string]string `json:"arguments,omitempty"`
}

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

	// Plugins allows overriding binder plugin configuration. Keys are plugin names.
	// Built-in plugins can be disabled, reordered, or have their arguments changed.
	// Built-in plugins: volumebinding, dynamicresources, gpusharing.
	// +kubebuilder:validation:Optional
	Plugins map[string]PluginConfig `json:"plugins,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the binder
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

func (b *Binder) SetDefaultsWhereNeeded(replicaCount *int32, globalVPA *common.VPASpec) {
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

	b.setDefaultPlugins()

	if b.VPA == nil {
		b.VPA = globalVPA
	}
}

func (b *Binder) setDefaultPlugins() {
	binderPluginConfig := DefaultPluginsConfig(ptr.Deref(b.VolumeBindingTimeoutSeconds, DefaultBindTimeoutSeconds),
		ptr.Deref(b.CDIEnabled, DefaultCDIEnabled))

	for name, userBinderConfig := range b.Plugins {
		defaultPluginConfig, found := binderPluginConfig[name]
		if found {
			//Merge default plugin config with user plugin config
			if userBinderConfig.Enabled != nil {
				defaultPluginConfig.Enabled = ptr.To(*userBinderConfig.Enabled)
			}
			if userBinderConfig.Priority != nil {
				defaultPluginConfig.Priority = ptr.To(*userBinderConfig.Priority)
			}
			if userBinderConfig.Arguments != nil {
				defaultPluginConfig.Arguments = copyStringMap(userBinderConfig.Arguments)
			}
			binderPluginConfig[name] = defaultPluginConfig
		} else {
			//If user set plugin but not the enabled parameter, default to enabled
			if userBinderConfig.Enabled == nil {
				userBinderConfig.Enabled = ptr.To(true)
			}
			binderPluginConfig[name] = userBinderConfig
		}
	}

	b.Plugins = binderPluginConfig
}

func DefaultPluginsConfig(bindTimeoutSeconds int, cdiEnabled bool) map[string]PluginConfig {
	return map[string]PluginConfig{
		VolumeBindingPluginName: {
			Enabled:  ptr.To(true),
			Priority: ptr.To(defaultPluginPriorities[VolumeBindingPluginName]),
			Arguments: map[string]string{
				BindTimeoutSecondsArgument: strconv.Itoa(bindTimeoutSeconds),
			},
		},
		DynamicResourcesPluginName: {
			Enabled:  ptr.To(true),
			Priority: ptr.To(defaultPluginPriorities[DynamicResourcesPluginName]),
			Arguments: map[string]string{
				BindTimeoutSecondsArgument: strconv.Itoa(bindTimeoutSeconds),
			},
		},
		GPUSharingPluginName: {
			Enabled:  ptr.To(true),
			Priority: ptr.To(defaultPluginPriorities[GPUSharingPluginName]),
			Arguments: map[string]string{
				CDIEnabledArgument: strconv.FormatBool(cdiEnabled),
			},
		},
	}
}

func copyStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
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

	// ReservationPodSecurityContext specifies the pod-level security context for reservation pods
	// +kubebuilder:validation:Optional
	ReservationPodSecurityContext *v1.PodSecurityContext `json:"reservationPodSecurityContext,omitempty"`

	// ReservationContainerSecurityContext specifies the container-level security context for reservation pod containers
	// +kubebuilder:validation:Optional
	ReservationContainerSecurityContext *v1.SecurityContext `json:"reservationContainerSecurityContext,omitempty"`
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
