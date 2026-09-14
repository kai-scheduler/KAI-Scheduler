// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestValidateContainerExists(t *testing.T) {
	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Name: "main"},
			},
			InitContainers: []v1.Container{
				{Name: "init-main"},
			},
		},
	}

	tests := []struct {
		name              string
		containerName     string
		wantErrContaining string
	}{
		{
			name:          "regular container found",
			containerName: "main",
		},
		{
			name:          "init container found",
			containerName: "init-main",
		},
		{
			name:              "container not found",
			containerName:     "missing",
			wantErrContaining: "container missing not found in pod spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContainerExists(pod, tt.containerName)
			assertErrorContains(t, err, tt.wantErrContaining)
		})
	}
}
