// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package pod_group_controller

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

const (
	imageName                      = "podgroupcontroller"
	defaultValidatingWebhookPrefix = "kai-podgroup-validation-"
)

type PodGroupController struct {
	Service *common.Service `json:"service,omitempty"`

	// ControllerService describes the service for the podgroup-controller
	// +kubebuilder:validation:Optional
	ControllerService *Service `json:"controllerService,omitempty"`

	// MaxConcurrentReconciles specifies the number of max concurrent reconcile workers
	// +kubebuilder:validation:Optional
	MaxConcurrentReconciles *int `json:"maxConcurrentReconciles,omitempty"`

	// Webhooks describes the configuration of the podgroup controller webhooks
	// +kubebuilder:validation:Optional
	Webhooks *PodGroupControllerWebhooks `json:"webhooks,omitempty"`

	// Replicas specifies the number podgroup controller replicas
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the pod group controller
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

func (pg *PodGroupController) SetDefaultsWhereNeeded(replicaCount *int32, globalVPA *common.VPASpec) {
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

	pg.ControllerService = common.SetDefault(pg.ControllerService, &Service{})
	pg.ControllerService.SetDefaultsWhereNeeded()

	pg.Replicas = common.SetDefault(pg.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))

	pg.Webhooks = common.SetDefault(pg.Webhooks, &PodGroupControllerWebhooks{})
	pg.Webhooks.SetDefaultsWhereNeeded()

	if pg.VPA == nil {
		pg.VPA = globalVPA
	}
}

type Service struct {
	// Webhook specifies the webhook service spec
	// +kubebuilder:validation:Optional
	Webhook *PortMapping `json:"webhook,omitempty"`
}

func (s *Service) SetDefaultsWhereNeeded() {
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

type PodGroupControllerWebhooks struct {
	// EnableValidation enables the validation webhook for the pod group controller
	// +kubebuilder:validation:Optional
	EnableValidation *bool `json:"enableValidation,omitempty"`

	// WebhookConfigurationNamePrefix is the prefix used for webhook configuration names
	// +kubebuilder:validation:Optional
	WebhookConfigurationNamePrefix *string `json:"webhookConfigurationNamePrefix,omitempty"`
}

func (q *PodGroupControllerWebhooks) SetDefaultsWhereNeeded() {
	q.EnableValidation = common.SetDefault(q.EnableValidation, ptr.To(true))
	q.WebhookConfigurationNamePrefix = common.SetDefault(q.WebhookConfigurationNamePrefix, ptr.To(defaultValidatingWebhookPrefix))
}
