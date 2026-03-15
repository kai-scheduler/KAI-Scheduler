// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package common

import (
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
)

// VPASpec defines Vertical Pod Autoscaler configuration
type VPASpec struct {
	// Enabled specifies if VPA should be enabled
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// UpdatePolicy controls when and how VPA applies changes to pod resources
	// +kubebuilder:validation:Optional
	UpdatePolicy *vpav1.PodUpdatePolicy `json:"updatePolicy,omitempty"`

	// ResourcePolicy controls how VPA computes recommended resources for containers
	// +kubebuilder:validation:Optional
	ResourcePolicy *vpav1.PodResourcePolicy `json:"resourcePolicy,omitempty"`
}

func (v *VPASpec) SetDefaultsWhereNeeded() {
	if v.Enabled == nil {
		v.Enabled = ptr.To(false)
	}
	if v.UpdatePolicy == nil {
		v.UpdatePolicy = &vpav1.PodUpdatePolicy{}
	}
	if v.UpdatePolicy.UpdateMode == nil {
		mode := vpav1.UpdateModeInPlaceOrRecreate
		v.UpdatePolicy.UpdateMode = &mode
	}
}
