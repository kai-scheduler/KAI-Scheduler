// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMaxResources(t *testing.T) {
	tests := []struct {
		name  string
		left  v1.ResourceList
		right v1.ResourceList
		want  v1.ResourceList
	}{
		{
			name:  "per resource maximum",
			left:  v1.ResourceList{v1.ResourceCPU: resource.MustParse("2"), v1.ResourceMemory: resource.MustParse("1Gi")},
			right: v1.ResourceList{v1.ResourceCPU: resource.MustParse("5"), v1.ResourceMemory: resource.MustParse("512Mi")},
			want:  v1.ResourceList{v1.ResourceCPU: resource.MustParse("5"), v1.ResourceMemory: resource.MustParse("1Gi")},
		},
		{
			name:  "a resource on one side only is kept",
			left:  v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
			right: v1.ResourceList{v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2")},
			want: v1.ResourceList{
				v1.ResourceCPU:                    resource.MustParse("1"),
				v1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
			},
		},
		{
			name:  "empty left takes the right",
			left:  v1.ResourceList{},
			right: v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")},
			want:  v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")},
		},
		{
			name:  "nil left takes the right",
			left:  nil,
			right: v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")},
			want:  v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaxResources(tt.left, tt.right)
			assert.Len(t, got, len(tt.want))
			for name, want := range tt.want {
				gotQuantity := got[name]
				assert.Zero(t, gotQuantity.Cmp(want),
					"resource %s: got %s, want %s", name, gotQuantity.String(), want.String())
			}
		})
	}
}
