// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name              string
		pod               *v1.Pod
		GPUSharingEnabled bool
		error             error
	}{
		{
			name: "GPU sharing disabled, whole GPU pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			GPUSharingEnabled: false,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, whole GPU pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing disabled, GPU sharing pod - fraction",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			GPUSharingEnabled: false,
			error: fmt.Errorf("attempting to create a pod test-namespace/test-pod with gpu " +
				"sharing request, while GPU sharing is disabled"),
		},
		{
			name: "GPU sharing enabled, GPU sharing pod - fraction",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing disabled, GPU sharing pod - memory",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "1024",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			GPUSharingEnabled: false,
			error: fmt.Errorf("attempting to create a pod test-namespace/test-pod with gpu " +
				"sharing request, while GPU sharing is disabled"),
		},
		{
			name: "GPU sharing enabled, GPU sharing pod - memory",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "1024",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().WithRuntimeObjects(tt.pod).Build()
			gpuSharingPlugin := New(kubeClient, tt.GPUSharingEnabled)
			err := gpuSharingPlugin.Validate(tt.pod)
			if err == nil && tt.error != nil {
				t.Errorf("Validate() expected and error but actual is nil")
				return
			}
			if err != nil && tt.error == nil {
				t.Errorf("Validate() actual is nil but didn't expect and error. Error: %v", err)
				return
			}
			if tt.error != nil && err.Error() != tt.error.Error() {
				t.Errorf("Validate()\nactual: %v\nexpected: %v\n", err, tt.error)
				return
			}
		})
	}
}
