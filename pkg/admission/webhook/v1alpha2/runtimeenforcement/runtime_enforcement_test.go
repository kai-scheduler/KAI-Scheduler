// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package runtimeenforcement

import (
	"testing"

	"github.com/kai-scheduler/kai-scheduler/pkg/apis/client/clientset/versioned/scheme"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	ocpconf "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
)

func TestMutate(t *testing.T) {
	sc := scheme.Scheme
	utilruntime.Must(ocpconf.AddToScheme(sc))

	tests := []struct {
		name                   string
		gpuPodRuntimeClassName string
		incomingPod            *v1.Pod
		expectedOutboundPod    *v1.Pod
		expectedError          error
	}{
		{
			name:                   "pod without GPU requests",
			gpuPodRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod:            &v1.Pod{},
			expectedOutboundPod:    &v1.Pod{},
			expectedError:          nil,
		},
		{
			name:                   "pod with a fractional GPU request",
			gpuPodRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
			},
			expectedOutboundPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To(constants.DefaultRuntimeClassName),
				},
			},
			expectedError: nil,
		},
		{
			name:                   "pod with a whole GPU request",
			gpuPodRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedOutboundPod: &v1.Pod{
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To(constants.DefaultRuntimeClassName),
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:                   "empty gpuPodRuntimeClassName skips runtimeClass injection",
			gpuPodRuntimeClassName: "",
			incomingPod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedOutboundPod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:                   "pod with GPU request and runtimeClassName set",
			gpuPodRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod: &v1.Pod{
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To("custom-runtime"),
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedOutboundPod: &v1.Pod{
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To("custom-runtime"),
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("1")},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.gpuPodRuntimeClassName)
			err := p.Mutate(tt.incomingPod)
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedOutboundPod, tt.incomingPod)
		})
	}
}
