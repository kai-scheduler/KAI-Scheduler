// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package common

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

type Service struct {
	// Enabled defines whether the service should be deployed
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Image is the configuration of the service image
	// +kubebuilder:validation:Optional
	Image *Image `json:"image,omitempty"`

	// Resources describes the resource requirements for the service pods
	// +kubebuilder:validation:Optional
	Resources *Resources `json:"resources,omitempty"`

	// ClientConfig specifies the configuration of k8s client
	// +kubebuilder:validation:Optional
	K8sClientConfig *K8sClientConfig `json:"k8sClientConfig,omitempty"`

	// Affinity defines affinity for the service pods
	// +kubebuilder:validation:Optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
}

func (s *Service) SetDefaultsWhereNeeded(imageName string) {
	s.Enabled = SetDefault(s.Enabled, ptr.To(true))

	s.Image = SetDefault(s.Image, &Image{})
	s.Image.Name = SetDefault(s.Image.Name, ptr.To(imageName))
	s.Image.SetDefaultsWhereNeeded()

	s.Resources = SetDefault(s.Resources, &Resources{})
	s.Resources.SetDefaultsWhereNeeded()

	s.K8sClientConfig = SetDefault(s.K8sClientConfig, &K8sClientConfig{})
	s.K8sClientConfig.SetDefaultsWhereNeeded()
}
