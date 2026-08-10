// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"fmt"
	"strings"
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
									constants.NvidiaGpuResource: resource.MustParse("1"),
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
									constants.NvidiaGpuResource: resource.MustParse("1"),
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
		{
			name: "GPU sharing disabled, NvFractions request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: false,
			error: fmt.Errorf("attempting to create a pod test-namespace/test-pod with gpu " +
				"sharing request, while GPU sharing is disabled"),
		},
		{
			name: "GPU sharing enabled, NvFractions request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, NvFractions request matching gpu-memory annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "1024",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, NvFractions request with limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix:   "2Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, NvFractions request not matching gpu-memory annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "2048",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error: fmt.Errorf(
				"NvFractions memory request (1Gi) does not match %s annotation value (2048 MiB)",
				constants.GpuMemory,
			),
		},
		{
			name: "GPU sharing enabled, NvFractions request with gpu-fraction annotation - NvFractions wins",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, NvFractions limit with gpu-fraction annotation - not allowed",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error: fmt.Errorf("cannot combine %s limit annotation with %s or %s annotation",
				constants.NvFractionsAnnotationPrefix, constants.GpuFraction, constants.GpuMemory),
		},
		{
			name: "GPU sharing enabled, NvFractions limit with gpu-memory annotation - not allowed",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "1024",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error: fmt.Errorf("cannot combine %s limit annotation with %s or %s annotation",
				constants.NvFractionsAnnotationPrefix, constants.GpuFraction, constants.GpuMemory),
		},
		{
			name: "GPU sharing enabled, invalid NvFractions request value",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "0Mi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error: fmt.Errorf(
				"%s annotation value must be a valid Kubernetes memory quantity greater than 0",
				constants.NvFractionsAnnotationPrefix+"main"+constants.NvFractionsMemoryRequestSuffix,
			),
		},
		{
			name: "GPU sharing enabled, NvFractions request greater than limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "4Gi",
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryLimitSuffix:   "2Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             fmt.Errorf("invalid fraction request for container main: request is greater than limit"),
		},
		{
			name: "GPU sharing enabled, NvFractions request references non-existent container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "missing-container" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error: fmt.Errorf(
				"container missing-container not found in pod spec, but a fractional annotation referencing it was found",
			),
		},
		{
			name: "GPU sharing enabled, multiple containers with NvFractions requests",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix:    "1Gi",
						constants.NvFractionsAnnotationPrefix + "sidecar" + constants.NvFractionsMemoryRequestSuffix: "512Mi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "main",
							Resources: v1.ResourceRequirements{},
						},
						{
							Name:      "sidecar",
							Resources: v1.ResourceRequirements{},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             fmt.Errorf("currently, kai doesn't support multiple containers with fractional GPU requests"),
		},
		{
			name: "GPU sharing enabled, NvFractions request with whole GPU limit",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.NvFractionsAnnotationPrefix + "main" + constants.NvFractionsMemoryRequestSuffix: "1Gi",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "main",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit"),
		},
		{
			name: "GPU sharing enabled, allow whole GPU resource limit of 2",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: resource.MustParse("2"),
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
			name: "GPU sharing enabled, forbid GPU fraction with GPU memory annotation",
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
			GPUSharingEnabled: true,
			error:             fmt.Errorf("cannot request both gpu-fraction and GPU memory request"),
		},
		{
			name: "GPU sharing enabled, allow multiple containers with whole GPU resource request",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: resource.MustParse("1"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: resource.MustParse("2"),
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
			name: "GPU sharing enabled, forbid GPU resource limit with GPU memory annotation",
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
									constants.NvidiaGpuResource: *resource.NewMilliQuantity(1, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit"),
		},
		{
			name: "GPU sharing enabled, forbid GPU resource limit in sidecar with GPU memory annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.GpuMemory: "1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      "DistractionContainer",
							Resources: v1.ResourceRequirements{},
						},
						{
							Name: "SneakyGPUContainer",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: *resource.NewMilliQuantity(1, resource.DecimalSI),
								},
							},
						},
					},
				},
			},
			GPUSharingEnabled: true,
			error:             fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit"),
		},
		{
			name: "GPU sharing enabled, forbid negative GPU memory annotation",
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
			GPUSharingEnabled: true,
			error:             fmt.Errorf("gpu-memory annotation value must be a positive integer greater than 0"),
		},
		{
			name: "GPU sharing enabled, forbid negative GPU fraction annotation",
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
			GPUSharingEnabled: true,
			error:             fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0"),
		},
		{
			name: "GPU sharing enabled, forbid GPU fraction greater than 1.0",
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
			GPUSharingEnabled: true,
			error:             fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0"),
		},
		{
			name: "GPU sharing enabled, forbid GPU fraction count without fractions or memory",
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
			GPUSharingEnabled: true,
			error: fmt.Errorf(
				"cannot request multiple fractional devices without specifying fraction details (portion or memory)",
			),
		},
		{
			name: "GPU sharing enabled, forbid invalid GPU fraction count with fractions",
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
			GPUSharingEnabled: true,
			error:             fmt.Errorf("fraction count annotation value must be a positive integer greater than 0"),
		},
		{
			name: "GPU sharing enabled, allow GPU fraction count with memory",
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
			GPUSharingEnabled: true,
			error:             nil,
		},
		{
			name: "GPU sharing enabled, allow GPU fraction count with fractions",
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

func TestMutate(t *testing.T) {
	tests := []struct {
		name               string
		pod                *v1.Pod
		expectError        bool
		errorContains      string
		validateMutatedPod func(t *testing.T, pod *v1.Pod)
	}{
		{
			name: "pod with no containers - should return nil without mutation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Pod should remain unchanged
				if len(pod.Spec.Containers) != 0 {
					t.Errorf("Expected no containers, got %d", len(pod.Spec.Containers))
				}
			},
		},
		{
			name: "pod without GPU fraction request - should return nil without mutation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									constants.NvidiaGpuResource: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Pod should not have GPU sharing env vars
				container := pod.Spec.Containers[0]
				for _, env := range container.Env {
					if env.Name == constants.NvidiaVisibleDevices || env.Name == "GPU_PORTION" {
						t.Errorf("Unexpected GPU sharing env var %s added to non-GPU-fraction pod", env.Name)
					}
				}
			},
		},
		{
			name: "pod with GPU fraction - should mutate with default container",
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
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Check that config map annotation was added
				if _, found := pod.Annotations["runai/shared-gpu-configmap"]; !found {
					t.Errorf("Expected shared-gpu-configmap annotation to be added")
				}

				// Check that env vars were added to the container
				container := pod.Spec.Containers[0]
				expectedEnvVars := []string{constants.NvidiaVisibleDevices, "RUNAI_NUM_OF_GPUS", "GPU_PORTION"}
				foundEnvVars := make(map[string]bool)
				for _, env := range container.Env {
					if contains(expectedEnvVars, env.Name) {
						foundEnvVars[env.Name] = true
						if env.ValueFrom == nil || env.ValueFrom.ConfigMapKeyRef == nil {
							t.Errorf("Expected env var %s to have ConfigMapKeyRef", env.Name)
						}
					}
				}
				for _, expectedVar := range expectedEnvVars {
					if !foundEnvVars[expectedVar] {
						t.Errorf("Expected env var %s to be added", expectedVar)
					}
				}

				// Check that volume was added
				if len(pod.Spec.Volumes) == 0 {
					t.Errorf("Expected at least one volume to be added")
				}
				foundVolume := false
				for _, volume := range pod.Spec.Volumes {
					if volume.VolumeSource.ConfigMap != nil {
						foundVolume = true
					}
				}
				if !foundVolume {
					t.Errorf("Expected ConfigMap volume to be added")
				}

				// Check that EnvFrom was added
				if len(container.EnvFrom) == 0 {
					t.Errorf("Expected EnvFrom to be added")
				}
				foundEnvFrom := false
				for _, envFrom := range container.EnvFrom {
					if envFrom.ConfigMapRef != nil {
						foundEnvFrom = true
					}
				}
				if !foundEnvFrom {
					t.Errorf("Expected ConfigMapRef in EnvFrom")
				}
			},
		},
		{
			name: "pod with GPU memory - should mutate with default container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuMemory: "4096",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Check that config map annotation was added
				if _, found := pod.Annotations["runai/shared-gpu-configmap"]; !found {
					t.Errorf("Expected shared-gpu-configmap annotation to be added")
				}

				// Check that env vars were added
				container := pod.Spec.Containers[0]
				if len(container.Env) == 0 {
					t.Errorf("Expected env vars to be added")
				}

				// Check that the NvFractions memory request annotation was added
				expectedAnnotationKey := constants.NvFractionsAnnotationPrefix + "test-container" + constants.NvFractionsMemoryRequestSuffix
				memoryRequest, found := pod.Annotations[expectedAnnotationKey]
				if !found {
					t.Errorf("Expected NvFractions memory request annotation to be added")
				}
				if memoryRequest != "4Gi" {
					t.Errorf("Expected NvFractions memory request annotation to be 4Gi, got %s", memoryRequest)
				}
			},
		},
		{
			name: "pod with dotted name keeps configmap refs and sanitizes volume name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test.pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction: "0.5",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				configMapPrefix, found := pod.Annotations["runai/shared-gpu-configmap"]
				if !found {
					t.Fatalf("Expected shared-gpu-configmap annotation to be added")
				}
				if !strings.Contains(configMapPrefix, ".") {
					t.Fatalf("Expected configmap annotation to preserve dotted pod name, got %q", configMapPrefix)
				}

				expectedCapabilitiesConfigMapName := configMapPrefix + "-0"
				expectedEnvVarsConfigMapName := expectedCapabilitiesConfigMapName + "-evar"

				var configMapVolume *v1.Volume
				for i := range pod.Spec.Volumes {
					volume := &pod.Spec.Volumes[i]
					if volume.ConfigMap != nil {
						configMapVolume = volume
						break
					}
				}
				if configMapVolume == nil {
					t.Fatalf("Expected ConfigMap volume to be added")
				}
				if strings.Contains(configMapVolume.Name, ".") {
					t.Fatalf("Expected sanitized volume name, got %q", configMapVolume.Name)
				}
				if configMapVolume.ConfigMap.LocalObjectReference.Name != expectedCapabilitiesConfigMapName {
					t.Fatalf("Expected volume configmap ref %q, got %q",
						expectedCapabilitiesConfigMapName, configMapVolume.ConfigMap.LocalObjectReference.Name)
				}

				container := pod.Spec.Containers[0]
				for _, env := range container.Env {
					if env.ValueFrom == nil || env.ValueFrom.ConfigMapKeyRef == nil {
						continue
					}
					if env.ValueFrom.ConfigMapKeyRef.LocalObjectReference.Name != expectedCapabilitiesConfigMapName {
						t.Fatalf("Expected env var configmap ref %q, got %q",
							expectedCapabilitiesConfigMapName,
							env.ValueFrom.ConfigMapKeyRef.LocalObjectReference.Name)
					}
				}

				foundEnvFrom := false
				for _, envFrom := range container.EnvFrom {
					if envFrom.ConfigMapRef == nil {
						continue
					}
					foundEnvFrom = true
					if envFrom.ConfigMapRef.LocalObjectReference.Name != expectedEnvVarsConfigMapName {
						t.Fatalf("Expected EnvFrom configmap ref %q, got %q",
							expectedEnvVarsConfigMapName,
							envFrom.ConfigMapRef.LocalObjectReference.Name)
					}
				}
				if !foundEnvFrom {
					t.Fatalf("Expected ConfigMapRef in EnvFrom")
				}
			},
		},
		{
			name: "pod with GPU fraction and specific container name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction:              "0.5",
						constants.GpuFractionContainerName: "gpu-container",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "sidecar",
						},
						{
							Name: "gpu-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Check that the second container (gpu-container) has the env vars
				gpuContainer := pod.Spec.Containers[1]
				foundNvidiaVar := false
				for _, env := range gpuContainer.Env {
					if env.Name == constants.NvidiaVisibleDevices {
						foundNvidiaVar = true
					}
				}
				if !foundNvidiaVar {
					t.Errorf("Expected NVIDIA_VISIBLE_DEVICES to be added to gpu-container")
				}

				// Check that the sidecar doesn't have GPU env vars
				sidecarContainer := pod.Spec.Containers[0]
				for _, env := range sidecarContainer.Env {
					if env.Name == constants.NvidiaVisibleDevices {
						t.Errorf("Unexpected NVIDIA_VISIBLE_DEVICES in sidecar container")
					}
				}
			},
		},
		{
			name: "pod with GPU fraction and non-existent container name - should return error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction:              "0.5",
						constants.GpuFractionContainerName: "non-existent",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
				},
			},
			expectError:   true,
			errorContains: "failed to get fraction container ref",
		},
		{
			name: "pod with GPU fraction and init container",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						constants.GpuFraction:              "0.5",
						constants.GpuFractionContainerName: "init-gpu",
					},
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-gpu",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "main-container",
						},
					},
				},
			},
			expectError: false,
			validateMutatedPod: func(t *testing.T, pod *v1.Pod) {
				// Check that config map name has init container indicator ("i0")
				configMapName := pod.Annotations["runai/shared-gpu-configmap"]
				if configMapName == "" {
					t.Errorf("Expected config map annotation to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().Build()
			gpuSharingPlugin := New(kubeClient, true)

			err := gpuSharingPlugin.Mutate(tt.pod)

			if tt.expectError {
				if err == nil {
					t.Errorf("Mutate() expected an error but got nil")
					return
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("Mutate() error = %v, expected to contain %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Mutate() unexpected error: %v", err)
				return
			}

			if tt.validateMutatedPod != nil {
				tt.validateMutatedPod(t, tt.pod)
			}
		})
	}
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
