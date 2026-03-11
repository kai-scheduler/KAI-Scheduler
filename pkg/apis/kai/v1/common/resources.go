// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Resources v1.ResourceRequirements

func (r *Resources) SetDefaultsWhereNeeded() {
	if r.Requests == nil {
		r.Requests = v1.ResourceList{}
	}
	if r.Limits == nil {
		r.Limits = v1.ResourceList{}
	}

	if _, found := r.Requests[v1.ResourceCPU]; !found {
		r.Requests[v1.ResourceCPU] = resource.MustParse("50m")
	}
	if _, found := r.Requests[v1.ResourceMemory]; !found {
		r.Requests[v1.ResourceMemory] = resource.MustParse("256Mi")
	}
	if _, found := r.Limits[v1.ResourceCPU]; !found {
		r.Limits[v1.ResourceCPU] = resource.MustParse("100m")
	}
	if _, found := r.Limits[v1.ResourceMemory]; !found {
		r.Limits[v1.ResourceMemory] = resource.MustParse("512Mi")
	}
}

func DefaultServiceResources() *Resources {
	resources := Resources{}
	resources.SetDefaultsWhereNeeded()
	return &resources
}
