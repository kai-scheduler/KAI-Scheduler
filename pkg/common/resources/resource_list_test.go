// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestIsMemoryResource(t *testing.T) {
	tests := []struct {
		name v1.ResourceName
		want bool
	}{
		{name: v1.ResourceMemory, want: true},
		{name: "hugepages-2Mi", want: true},
		{name: "hugepages-1Gi", want: true},
		{name: v1.ResourceCPU},
		{name: v1.ResourceEphemeralStorage},
		{name: "nvidia.com/gpu"},
		{name: "example.com/memory"},
		{name: ""},
	}
	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			if got := IsMemoryResource(test.name); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
