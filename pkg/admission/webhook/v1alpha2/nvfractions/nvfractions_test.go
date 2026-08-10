// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

func nvFractionsRequestKey(container string) string {
	return resources.CalcGpuFractionAnnotationForContainer(container)
}

func TestMutateConvertsLegacyGpuMemoryWithoutConfigmap(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{constants.GpuMemory: "2000"}},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
	}

	err := New().Mutate(pod)
	assert.NoError(t, err)

	assert.NotEmpty(t, pod.Annotations[nvFractionsRequestKey("container-0")])
	// Decoupled from configmap: no volumes or configmap-backed env vars are added.
	assert.Empty(t, pod.Spec.Volumes)
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			assert.Nil(t, env.ValueFrom)
		}
	}
}

func TestMutateConvertsLegacyGpuMemoryForNamedContainer(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.GpuMemory:                "2000",
			constants.GpuFractionContainerName: "container-1",
		}},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}, {Name: "container-1"}}},
	}

	err := New().Mutate(pod)
	assert.NoError(t, err)

	assert.NotEmpty(t, pod.Annotations[nvFractionsRequestKey("container-1")])
	assert.Empty(t, pod.Annotations[nvFractionsRequestKey("container-0")])
}

func TestMutateNoOpWithoutFractionRequest(t *testing.T) {
	pod := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}}}
	err := New().Mutate(pod)
	assert.NoError(t, err)
	assert.Empty(t, pod.Annotations)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		annotations     map[string]string
		wantErrContains string
	}{
		{
			name:        "valid nvfractions request",
			annotations: map[string]string{nvFractionsRequestKey("container-0"): "1Gi"},
		},
		{
			name:        "valid legacy gpu-fraction (portion) request",
			annotations: map[string]string{constants.GpuFraction: "0.5"},
		},
		{
			name:        "valid legacy gpu-memory request",
			annotations: map[string]string{constants.GpuMemory: "2000"},
		},
		{
			name: "valid legacy gpu-memory with matching container-name",
			annotations: map[string]string{
				constants.GpuMemory:                "2000",
				constants.GpuFractionContainerName: "container-0",
			},
		},
		{
			name: "rejects both gpu-fraction and gpu-memory",
			annotations: map[string]string{
				constants.GpuFraction: "0.5",
				constants.GpuMemory:   "2000",
			},
			wantErrContains: "cannot request both gpu-fraction and GPU memory request",
		},
		{
			name: "rejects container-name mismatch with nvfractions annotation",
			annotations: map[string]string{
				nvFractionsRequestKey("container-0"): "1Gi",
				constants.GpuFractionContainerName:   "other",
			},
			wantErrContains: "does not match container name",
		},
		{
			name:            "references missing container",
			annotations:     map[string]string{nvFractionsRequestKey("missing"): "1Gi"},
			wantErrContains: "not found in pod spec",
		},
		{
			name:        "no fraction request",
			annotations: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "container-0"}}},
			}

			err := New().Validate(pod)
			if tt.wantErrContains != "" {
				assert.ErrorContains(t, err, tt.wantErrContains)
				return
			}
			assert.NoError(t, err)
		})
	}
}
