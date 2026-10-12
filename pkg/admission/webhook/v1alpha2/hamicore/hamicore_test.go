// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package hamicore

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/api/constants"
)

func TestMutate(t *testing.T) {
	tests := []struct {
		name               string
		pod                *v1.Pod
		expectError        bool
		expectEnvVar       bool
		targetContainerIdx int
	}{
		{
			name: "no containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{Containers: []v1.Container{}},
			},
		},
		{
			name: "non-fraction pod is skipped",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "c"}},
				},
			},
		},
		{
			name: "fraction pod without configmap annotation returns error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod", Namespace: "ns",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "c"}},
				},
			},
			expectError: true,
		},
		{
			name: "fraction pod with configmap annotation gets env var",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod", Namespace: "ns",
					Annotations: map[string]string{
						constants.GpuFraction:                   "0.5",
						constants.GpuSharingConfigMapAnnotation: "pod-shared-gpu",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{Name: "c"}},
				},
			},
			expectEnvVar:       true,
			targetContainerIdx: 0,
		},
		{
			name: "fraction pod with named container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod", Namespace: "ns",
					Annotations: map[string]string{
						constants.GpuFraction:                   "0.5",
						constants.GpuFractionContainerName:      "gpu",
						constants.GpuSharingConfigMapAnnotation: "pod-shared-gpu",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "sidecar"},
						{Name: "gpu"},
					},
				},
			},
			expectEnvVar:       true,
			targetContainerIdx: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New()

			err := plugin.Mutate(tt.pod)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.expectEnvVar {
				for i, c := range tt.pod.Spec.Containers {
					for _, env := range c.Env {
						if env.Name == common.CudaDeviceMemoryLimit {
							t.Errorf("container %d should not have %s env var", i, common.CudaDeviceMemoryLimit)
						}
					}
				}
				return
			}

			container := tt.pod.Spec.Containers[tt.targetContainerIdx]
			found := false
			for _, env := range container.Env {
				if env.Name == common.CudaDeviceMemoryLimit {
					found = true
					if env.ValueFrom == nil || env.ValueFrom.ConfigMapKeyRef == nil {
						t.Errorf("expected ConfigMapKeyRef for %s", common.CudaDeviceMemoryLimit)
					} else if env.ValueFrom.ConfigMapKeyRef.Optional == nil || !*env.ValueFrom.ConfigMapKeyRef.Optional {
						t.Errorf("expected Optional=true for %s", common.CudaDeviceMemoryLimit)
					}
				}
			}
			if !found {
				t.Errorf("expected %s env var on container %d", common.CudaDeviceMemoryLimit, tt.targetContainerIdx)
			}
		})
	}
}
