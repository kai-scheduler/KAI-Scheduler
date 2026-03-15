// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package scheduler

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultImageName = "scheduler"
)

type Scheduler struct {
	Service *common.Service `json:"service,omitempty"`

	// GOGC configures the GOGC environment variable for the scheduler container
	// +kubebuilder:validation:Optional
	GOGC *int `json:"GOGC,omitempty"`

	// SchedulerService specifies the service configuration for the scheduler
	// +kubebuilder:validation:Optional
	SchedulerService *Service `json:"schedulerService,omitempty"`

	// Replicas specifies the number of replicas of the scheduler service
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the scheduler
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

func (s *Scheduler) SetDefaultsWhereNeeded(replicaCount *int32, globalVPA *common.VPASpec) {
	s.Service = common.SetDefault(s.Service, &common.Service{})

	s.Service.Resources = common.SetDefault(s.Service.Resources, &common.Resources{})
	if s.Service.Resources.Requests == nil {
		s.Service.Resources.Requests = v1.ResourceList{}
	}
	if s.Service.Resources.Limits == nil {
		s.Service.Resources.Limits = v1.ResourceList{}
	}

	if _, found := s.Service.Resources.Requests[v1.ResourceCPU]; !found {
		s.Service.Resources.Requests[v1.ResourceCPU] = resource.MustParse("250m")
	}
	if _, found := s.Service.Resources.Requests[v1.ResourceMemory]; !found {
		s.Service.Resources.Requests[v1.ResourceMemory] = resource.MustParse("512Mi")
	}
	if _, found := s.Service.Resources.Limits[v1.ResourceCPU]; !found {
		s.Service.Resources.Limits[v1.ResourceCPU] = resource.MustParse("700m")
	}
	if _, found := s.Service.Resources.Limits[v1.ResourceMemory]; !found {
		s.Service.Resources.Limits[v1.ResourceMemory] = resource.MustParse("512Mi")
	}

	s.Service.SetDefaultsWhereNeeded(defaultImageName)
	s.GOGC = common.SetDefault(s.GOGC, ptr.To(400))

	s.SchedulerService = common.SetDefault(s.SchedulerService, &Service{})
	s.SchedulerService.SetDefaultsWhereNeeded()

	s.Replicas = common.SetDefault(s.Replicas, ptr.To(ptr.Deref(replicaCount, 1)))

	if s.VPA == nil {
		s.VPA = globalVPA
	}
	if s.VPA != nil {
		s.VPA.SetDefaultsWhereNeeded()
	}
}

// Service defines configuration for the scheduler service
type Service struct {
	// Type specifies the service type
	// +kubebuilder:validation:Optional
	Type *v1.ServiceType `json:"type,omitempty"`

	// Port specifies the service port
	// +kubebuilder:validation:Optional
	Port *int `json:"port,omitempty"`

	// TargetPort specifies the target port in the container
	// +kubebuilder:validation:Optional
	TargetPort *int `json:"targetPort,omitempty"`
}

func (service *Service) SetDefaultsWhereNeeded() {
	service.Type = common.SetDefault(service.Type, ptr.To(v1.ServiceTypeClusterIP))
	service.Port = common.SetDefault(service.Port, ptr.To(8080))
	service.TargetPort = common.SetDefault(service.TargetPort, ptr.To(8080))
}
