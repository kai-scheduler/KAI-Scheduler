// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestValidateGpuMemoryPortionLimitAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		containers  []v1.Container
		expectErr   bool
	}{
		{
			name:        "no portion limit annotation",
			annotations: map[string]string{},
			containers:  []v1.Container{{Name: "container-0"}},
			expectErr:   false,
		},
		{
			name: "portion limit without gpu-fraction",
			annotations: map[string]string{
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "valid portion limit greater than gpu-fraction",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  false,
		},
		{
			name: "portion limit smaller than gpu-fraction",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.4",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "portion limit equal to gpu-fraction",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.5",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "portion limit not smaller than 1",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "1",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "portion limit with more than 5 decimal digits",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.123456",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "portion limit with exactly 5 decimal digits",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.81234",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  false,
		},
		{
			name: "portion limit container name mismatches default fraction container",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				CalcGpuMemoryPortionLimitAnnotationForContainer("other-container"): "0.8",
			},
			containers: []v1.Container{{Name: "container-0"}},
			expectErr:  true,
		},
		{
			name: "portion limit container name mismatches gpu-fraction-container-name",
			annotations: map[string]string{
				constants.GpuFraction:                                          "0.5",
				constants.GpuFractionContainerName:                             "container-1",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-0"): "0.8",
			},
			containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}},
			expectErr:  true,
		},
		{
			name: "portion limit container name matches gpu-fraction-container-name",
			annotations: map[string]string{
				constants.GpuFraction:                                          "0.5",
				constants.GpuFractionContainerName:                             "container-1",
				CalcGpuMemoryPortionLimitAnnotationForContainer("container-1"): "0.8",
			},
			containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}},
			expectErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       v1.PodSpec{Containers: tt.containers},
			}

			err := validateGpuMemoryPortionLimitAnnotation(pod)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
