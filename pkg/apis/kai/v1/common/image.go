// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package common

import (
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	DefaultRepositoryEnvVarName = "MS_REPOSITORY"
	DefaultTagEnvVarName        = "MS_TAG"
)

// Image is a struct describing a container image
type Image struct {
	// Repository is the repository/registry prefix for the image
	// +kubebuilder:validation:Optional
	Repository *string `json:"repository,omitempty"`

	// Name is the name of the image
	// +kubebuilder:validation:Optional
	Name *string `json:"name,omitempty"`

	// Tag is the tag of the image
	// +kubebuilder:validation:Optional
	Tag *string `json:"tag,omitempty"`

	// PullPolicy is the pull policy of the image
	// +kubebuilder:validation:Optional
	PullPolicy *v1.PullPolicy `json:"pullPolicy,omitempty"`
}

func (i *Image) SetDefaultsWhereNeeded() {
	i.Repository = SetDefault(i.Repository, ptr.To(os.Getenv(DefaultRepositoryEnvVarName)))
	i.Tag = SetDefault(i.Tag, ptr.To(os.Getenv(DefaultTagEnvVarName)))
	i.PullPolicy = SetDefault(i.PullPolicy, ptr.To(v1.PullIfNotPresent))
	i.Name = SetDefault(i.Name, ptr.To(""))
}

func (i *Image) Url() string {
	return fmt.Sprintf("%s/%s:%s", *i.Repository, *i.Name, *i.Tag)
}
