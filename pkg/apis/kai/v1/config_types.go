// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/admission"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/binder"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/node_scale_adjuster"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/pod_group_controller"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/pod_grouper"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/prometheus"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/queue_controller"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1/scheduler"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ConditionType string

const (
	ConditionTypeReconciling       ConditionType = "Reconciling"
	ConditionTypeDeployed          ConditionType = "Deployed"
	ConditionTypeAvailable         ConditionType = "Available"
	ConditionDependenciesFulfilled ConditionType = "DependenciesFulfilled"
	ConditionTypeReady             ConditionType = "Ready"
)

type ConditionReason string

const (
	Deployed                   ConditionReason = "deployed"
	Available                  ConditionReason = "available"
	Reconciled                 ConditionReason = "reconciled"
	Reconciling                ConditionReason = "reconciling"
	Ready                      ConditionReason = "ready"
	NotReady                   ConditionReason = "not_ready"
	DependenciesFulfilled      ConditionReason = "dependencies_fulfilled"
	DependenciesMissing        ConditionReason = "dependencies_missing"
	PrometheusConnected        ConditionReason = "prometheus_connected"
	PrometheusConnectionFailed ConditionReason = "prometheus_connection_failed"
)

// ConfigSpec defines the desired state of Config
type ConfigSpec struct {
	// Namespace is the namespace to create the operands in
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// Global defined global configuration of the system
	// +kubebuilder:validation:Optional
	Global *GlobalConfig `json:"global,omitempty"`

	// PodGrouper specifies configuration for the pod-grouper
	// +kubebuilder:validation:Optional
	PodGrouper *pod_grouper.PodGrouper `json:"podGrouper,omitempty"`

	// Binder specifies configuration for the binder
	// +kubebuilder:validation:Optional
	Binder *binder.Binder `json:"binder,omitempty"`

	// Admission holds KAI admission webhooks
	// +kubebuilder:validation:Optional
	Admission *admission.Admission `json:"admission,omitempty"`

	// Scheduler is the configuration for the scheduler microservice
	// +kubebuilder:validation:Optional
	Scheduler *scheduler.Scheduler `json:"scheduler,omitempty"`

	// QueueController specifies configuration for the queue controller
	// +kubebuilder:validation:Optional
	QueueController *queue_controller.QueueController `json:"queueController,omitempty"`

	// PodGroupController specifies configuration for the pod-group-controller
	// +kubebuilder:validation:Optional
	PodGroupController *pod_group_controller.PodGroupController `json:"podGroupController,omitempty"`

	// NodeScaleAdjuster specifies configuration for the node-scale-adjuster
	// +kubebuilder:validation:Optional
	NodeScaleAdjuster *node_scale_adjuster.NodeScaleAdjuster `json:"nodeScaleAdjuster,omitempty"`

	// Prometheus specifies configuration for Prometheus monitoring
	// +kubebuilder:validation:Optional
	Prometheus *prometheus.Prometheus `json:"prometheus,omitempty"`
}

func (c *ConfigSpec) SetDefaultsWhereNeeded() {
	if len(c.Namespace) == 0 {
		c.Namespace = constants.DefaultKAINamespace
	}
	c.Global = common.SetDefault(c.Global, &GlobalConfig{})
	c.Global.SetDefaultWhereNeeded()

	c.QueueController = common.SetDefault(c.QueueController, &queue_controller.QueueController{})
	c.QueueController.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.Binder = common.SetDefault(c.Binder, &binder.Binder{})
	c.Binder.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.PodGrouper = common.SetDefault(c.PodGrouper, &pod_grouper.PodGrouper{})
	c.PodGrouper.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.Scheduler = common.SetDefault(c.Scheduler, &scheduler.Scheduler{})
	c.Scheduler.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.PodGroupController = common.SetDefault(c.PodGroupController, &pod_group_controller.PodGroupController{})
	c.PodGroupController.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.Admission = common.SetDefault(c.Admission, &admission.Admission{})
	c.Admission.SetDefaultsWhereNeeded(c.Global.ReplicaCount)

	c.NodeScaleAdjuster = common.SetDefault(c.NodeScaleAdjuster, &node_scale_adjuster.NodeScaleAdjuster{})
	c.NodeScaleAdjuster.SetDefaultsWhereNeeded()

	c.Prometheus = common.SetDefault(c.Prometheus, &prometheus.Prometheus{})
	c.Prometheus.SetDefaultsWhereNeeded()
}

// ConfigStatus defines the observed state of Config
type ConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Config is the Schema for the configs API
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigSpec   `json:"spec,omitempty"`
	Status ConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigList contains a list of Config
type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Config `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Config{}, &ConfigList{})
}
