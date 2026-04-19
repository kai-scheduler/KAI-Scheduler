// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpurequesthandler

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestValidateGpuRequests(t *testing.T) {
	tests := []struct {
		name  string
		pod   *v1.Pod
		error error
	}{
		{
			name: "allow MPS and fractional GPU limits",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.MpsAnnotation: "true",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: *resource.NewMilliQuantity(500, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			error: nil,
		},
		{
			name: "allow MPS and fractional GPU annotation request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.MpsAnnotation: "true",
						constants.GpuFraction:   "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: nil,
		},
		{
			name: "block MPS without GPU request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.MpsAnnotation: "true",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("MPS is only supported with GPU fraction request"),
		},
		{
			name: "forbid GPU annotation and GPU request mismatch (fractional)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: *resource.NewMilliQuantity(2, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			error: fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit"),
		},
		{
			name: "forbid GPU resource request without resource limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									constants.GpuResource: *resource.NewMilliQuantity(2, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			error: fmt.Errorf("GPU is not not listed in resource limits"),
		},
		{
			name: "forbid fraction limit annotation without fraction request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuLimit: "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("cannot have GPU limit annotation without GPU fraction request"),
		},
		{
			name: "forbid fraction limit annotation without fraction request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: *resource.NewMilliQuantity(1500, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			error: fmt.Errorf("GPU resource limit annotation cannot be larger than 1"),
		},
		{
			name: "forbid GPU fraction with GPU Memory fraction annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
						constants.GpuMemory:   "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("cannot request both GPU and GPU memory"),
		},
		{
			name: "forbid GPU resource limit with GPU Memory fraction annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuMemory: "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.GpuResource: *resource.NewMilliQuantity(1, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			error: fmt.Errorf("cannot request both GPU and GPU memory"),
		},
		{
			name: "forbid negative GPU memory annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuMemory: "-1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("gpu-memory annotation value must be a positive integer greater than 0"),
		},
		{
			name: "forbid negative GPU fraction annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFraction: "-1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0"),
		},
		{
			name: "forbid GPU fraction which is greater than 1.0",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFraction: "1.2",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0"),
		},
		{
			name: "forbid GPU fraction count without fractions or memory",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionsNumDevices: "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf(
				"cannot request multiple fractional devices without specifying fraction portion or gpu-memory",
			),
		},
		{
			name: "forbid GPU fraction count without fractions or memory",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionsNumDevices: "1.2",
						constants.GpuFraction:            "0.2",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: fmt.Errorf("fraction count annotation value must be a positive integer greater than 0"),
		},
		{
			name: "allow GPU fraction count with memory",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionsNumDevices: "2",
						constants.GpuMemory:              "1000",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: nil,
		},
		{
			name: "allow GPU fraction count with fractions",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuFractionsNumDevices: "2",
						constants.GpuFraction:            "0.3",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			error: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGpuRequests(tt.pod)
			if err == nil && tt.error != nil {
				t.Errorf("ValidateGpuRequests() expected and error but actual is nil")
				return
			}
			if err != nil && tt.error == nil {
				t.Errorf("ValidateGpuRequests() actual is nil but didn't expect and error. Error: %v", err)
				return
			}
			if tt.error != nil && err.Error() != tt.error.Error() {
				t.Errorf("ValidateGpuRequests() actual: %v, expected: %v", err, tt.error)
				return
			}
		})
	}
}
