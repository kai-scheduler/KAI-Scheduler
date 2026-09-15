// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package runtimeenforcement

import (
	"testing"

	"github.com/kai-scheduler/api/client/clientset/versioned/scheme"
	"github.com/kai-scheduler/api/constants"
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
		name                        string
		gpuFractionRuntimeClassName string
		incomingPod                 *v1.Pod
		expectedOutboundPod         *v1.Pod
		expectedError               error
	}{
		{
			name:                        "pod without GPU requests",
			gpuFractionRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod:                 &v1.Pod{},
			expectedOutboundPod:         &v1.Pod{},
			expectedError:               nil,
		},
		{
			name:                        "pod with a fractional GPU request",
			gpuFractionRuntimeClassName: constants.DefaultRuntimeClassName,
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
			name:                        "pod with a gpu-memory annotation",
			gpuFractionRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2048"},
				},
			},
			expectedOutboundPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuMemory: "2048"},
				},
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To(constants.DefaultRuntimeClassName),
				},
			},
			expectedError: nil,
		},
		{
			name:                        "whole-GPU pod is not mutated",
			gpuFractionRuntimeClassName: constants.DefaultRuntimeClassName,
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
			name:                        "empty gpuFractionRuntimeClassName skips runtimeClass injection",
			gpuFractionRuntimeClassName: "",
			incomingPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
			},
			expectedOutboundPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
			},
			expectedError: nil,
		},
		{
			name:                        "fraction pod with runtimeClassName already set is preserved",
			gpuFractionRuntimeClassName: constants.DefaultRuntimeClassName,
			incomingPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To("custom-runtime"),
				},
			},
			expectedOutboundPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.GpuFraction: "0.5"},
				},
				Spec: v1.PodSpec{
					RuntimeClassName: ptr.To("custom-runtime"),
				},
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.gpuFractionRuntimeClassName)
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
