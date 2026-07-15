// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package queue_controller

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

const (
	imageName                      = "queuecontroller"
	defaultValidatingWebhookPrefix = "kai-queue-validation-"
)

type QueueController struct {
	Service *common.Service `json:"service,omitempty"`

	// ControllerService describes the service for the queue-controller
	// +kubebuilder:validation:Optional
	ControllerService *Service `json:"controllerService,omitempty"`

	// Webhooks describes the configuration of the queue controller webhooks
	// +kubebuilder:validation:Optional
	Webhooks *QueueControllerWebhooks `json:"webhooks,omitempty"`

	// Replicas specifies the number of replicas of the queue controller
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// MetricsNamespace specifies the namespace where metrics are exposed for the queue controller
	// +kubebuilder:validation:Optional
	MetricsNamespace *string `json:"metricsNamespace,omitempty"`

	// QueueLabelToMetricLabel maps queue label keys to metric label keys for metrics exposure
	// +kubebuilder:validation:Optional
	QueueLabelToMetricLabel *string `json:"queueLabelToMetricLabel,omitempty"`

	// QueueLabelToDefaultMetricValue maps queue label keys to default metric values when the label is absent
	// +kubebuilder:validation:Optional
	QueueLabelToDefaultMetricValue *string `json:"queueLabelToDefaultMetricValue,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the queue controller
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

func (q *QueueController) SetDefaultsWhereNeeded(replicaCount *int32, globalVPA *common.VPASpec) {
	q.Service = common.SetDefault(q.Service, &common.Service{})
	q.Service.SetDefaultsWhereNeeded(imageName)

	if _, found := q.Service.Resources.Requests[v1.ResourceCPU]; !found {
		q.Service.Resources.Requests[v1.ResourceCPU] = resource.MustParse("20m")
	}
	if _, found := q.Service.Resources.Requests[v1.ResourceMemory]; !found {
		q.Service.Resources.Requests[v1.ResourceMemory] = resource.MustParse("50Mi")
	}
	if _, found := q.Service.Resources.Limits[v1.ResourceCPU]; !found {
		q.Service.Resources.Limits[v1.ResourceCPU] = resource.MustParse("50m")
	}
	if _, found := q.Service.Resources.Limits[v1.ResourceMemory]; !found {
		q.Service.Resources.Limits[v1.ResourceMemory] = resource.MustParse("100Mi")
	}

	q.ControllerService = common.SetDefault(q.ControllerService, &Service{})
	q.ControllerService.SetDefaultsWhereNeeded()

	q.Replicas = common.SetDefault(q.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))

	q.Webhooks = common.SetDefault(q.Webhooks, &QueueControllerWebhooks{})
	q.Webhooks.SetDefaultsWhereNeeded()

	if q.VPA == nil {
		q.VPA = globalVPA
	}
}

type Service struct {
	// Metrics specifies the metrics service spec
	// +kubebuilder:validation:Optional
	Metrics *PortMapping `json:"metrics,omitempty"`

	// Webhook specifies the webhook service spec
	// +kubebuilder:validation:Optional
	Webhook *PortMapping `json:"webhook,omitempty"`
}

func (s *Service) SetDefaultsWhereNeeded() {
	s.Metrics = common.SetDefault(s.Metrics, &PortMapping{})
	s.Metrics.SetDefaultsWhereNeeded()

	s.Webhook = common.SetDefault(s.Webhook, &PortMapping{})
	s.Webhook.Port = common.SetDefault(s.Webhook.Port, ptr.To(443))
	s.Webhook.TargetPort = common.SetDefault(s.Webhook.TargetPort, ptr.To(9443))
	s.Webhook.Name = common.SetDefault(s.Webhook.Name, ptr.To("webhook"))
}

type PortMapping struct {
	// Port specifies the service port
	// +kubebuilder:validation:Optional
	Port *int `json:"port,omitempty"`

	// TargetPort specifies the pod container port
	// +kubebuilder:validation:Optional
	TargetPort *int `json:"targetPort,omitempty"`

	// Name specifies the name of the port
	// +kubebuilder:validation:Optional
	Name *string `json:"name,omitempty"`
}

func (p *PortMapping) SetDefaultsWhereNeeded() {
	p.Port = common.SetDefault(p.Port, ptr.To(8080))
	p.TargetPort = common.SetDefault(p.TargetPort, ptr.To(8080))
	p.Name = common.SetDefault(p.Name, ptr.To("metrics"))
}

type QueueControllerWebhooks struct {
	// EnableValidation enables the validation webhook for the queue controller
	// +kubebuilder:validation:Optional
	EnableValidation *bool `json:"enableValidation,omitempty"`

	// WebhookConfigurationNamePrefix is the prefix used for webhook configuration names
	// +kubebuilder:validation:Optional
	WebhookConfigurationNamePrefix *string `json:"webhookConfigurationNamePrefix,omitempty"`
}

func (q *QueueControllerWebhooks) SetDefaultsWhereNeeded() {
	q.EnableValidation = common.SetDefault(q.EnableValidation, ptr.To(true))
	q.WebhookConfigurationNamePrefix = common.SetDefault(q.WebhookConfigurationNamePrefix, ptr.To(defaultValidatingWebhookPrefix))
}
