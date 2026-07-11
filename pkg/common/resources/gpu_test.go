// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestSumGpuAllocation(t *testing.T) {
	tests := []struct {
		name string
		list v1.ResourceList
		want float64
	}{
		{
			name: "empty list",
			list: v1.ResourceList{},
			want: 0,
		},
		{
			name: "single extended GPU vendor",
			list: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("3")},
			want: 3,
		},
		{
			name: "sums across GPU vendors",
			list: v1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("2"),
				"amd.com/gpu":    resource.MustParse("1"),
			},
			want: 3,
		},
		{
			name: "fractional extended GPU",
			list: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("500m")},
			want: 0.5,
		},
		{
			name: "DRA GPU keyed by DeviceClass name",
			list: v1.ResourceList{"gpu.nvidia.com": resource.MustParse("2")},
			want: 2,
		},
		{
			name: "DRA GPU with a non-nvidia DeviceClass name",
			list: v1.ResourceList{"gpu.amd.com": resource.MustParse("1")},
			want: 1,
		},
		{
			name: "MIG profile counted as its GPU slice portion",
			list: v1.ResourceList{"nvidia.com/mig-1g.10gb": resource.MustParse("2")},
			want: 2,
		},
		{
			name: "larger MIG profile multiplies slices by quantity",
			list: v1.ResourceList{"nvidia.com/mig-3g.20gb": resource.MustParse("1")},
			want: 3,
		},
		{
			name: "NVIDIA GPU memory is excluded",
			list: v1.ResourceList{"nvidia.com/gpu.memory": resource.MustParse("40Gi")},
			want: 0,
		},
		{
			name: "KAI-synthesized GPU memory (run.ai/gpu.memory) is excluded",
			list: v1.ResourceList{"run.ai/gpu.memory": resource.MustParse("2000")},
			want: 0,
		},
		{
			name: "non-GPU resources are ignored",
			list: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("8Gi"),
			},
			want: 0,
		},
		{
			name: "malformed MIG resource is skipped",
			list: v1.ResourceList{"nvidia.com/mig-bad": resource.MustParse("1")},
			want: 0,
		},
		{
			name: "mixed extended, DRA and MIG GPUs are all summed",
			list: v1.ResourceList{
				"nvidia.com/gpu":         resource.MustParse("1"),
				"gpu.nvidia.com":         resource.MustParse("2"),
				"nvidia.com/mig-2g.10gb": resource.MustParse("1"),
				"nvidia.com/gpu.memory":  resource.MustParse("40Gi"),
				"run.ai/gpu.memory":      resource.MustParse("2000"),
				v1.ResourceCPU:           resource.MustParse("4"),
			},
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SumGpuAllocation(tt.list)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("SumGpuAllocation() = %v, want %v", got, tt.want)
			}
		})
	}
}
