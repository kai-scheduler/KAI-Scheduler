// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/configmap_info"
)

const (
	nvidiaVisibleDevices = "NVIDIA_VISIBLE_DEVICES"
)

func TestIsPreFilterRequired(t *testing.T) {
	cmp := NewConfigMapPredicate(nil)

	tests := []PreFilterTest{
		{
			name:     "Empty spec",
			pod:      &v1.Pod{},
			expected: false,
		},
		{
			name: "ConfigMap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "bla",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Unreferenced configMap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Unreferenced configMap and referenced configmap",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "bla-referenced",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
						{
							Name: "bla-referenced",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "non-ConfigMap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Referenced ConfigMap and non-configmap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "bla-configmap",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "bla-configmap",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "non-referenced ConfigMap and non-configmap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "bla-configmap",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "non-referenced ConfigMap and non-configmap volume",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "bla-configmap",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "env var",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Env: []v1.EnvVar{
							{
								ValueFrom: &v1.EnvVarSource{
									ConfigMapKeyRef: &v1.ConfigMapKeySelector{
										LocalObjectReference: v1.LocalObjectReference{
											Name: "bla",
										},
									},
								},
							},
						},
					}},
				},
			},
			expected: true,
		},
		{
			name: "env from",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						EnvFrom: []v1.EnvFromSource{
							{
								ConfigMapRef: &v1.ConfigMapEnvSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "bla",
									},
								},
							},
						},
					}},
				},
			},
			expected: true,
		},
		{
			name: "non-referenced configmap volume, env var",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Env: []v1.EnvVar{
							{
								ValueFrom: &v1.EnvVarSource{
									ConfigMapKeyRef: &v1.ConfigMapKeySelector{
										LocalObjectReference: v1.LocalObjectReference{
											Name: "bla",
										},
									},
								},
							},
						},
					}},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if cmp.isPreFilterRequired(test.pod) != test.expected {
				t.Errorf("Test %s: Expected %v, got %v", test.name, test.expected, cmp.isPreFilterRequired(test.pod))
			}
		})
	}
}

type PreFilterTest struct {
	name     string
	pod      *v1.Pod
	expected bool
}

func TestPreFilter(t *testing.T) {
	for _, test := range []FilterTest{
		{
			name:          "no configmaps, no required configmaps",
			configMaps:    nil,
			pod:           &v1.Pod{},
			expectedError: false,
		},
		{
			name:       "no configmaps, required configmaps",
			configMaps: nil,
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{{
								Name: "bla",
							}},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expectedError: true,
		},
		{
			name:       "no configmaps, non-referenced configmaps",
			configMaps: nil,
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "yes configmaps, required configmaps",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{
				"bla": {Namespace: "test", Name: "bla"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "bla",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "bla"},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "yes configmaps, required configmaps, different name",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{
				"configmap-name": {Namespace: "test", Name: "configmap-name"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "volume-name",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "volume-name",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "configmap-name"},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "some configmaps, required configmaps",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{
				"bla": {Namespace: "test", Name: "bla"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "bla",
								},
								{
									Name: "blu",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "bla"},
								},
							},
						},
						{
							Name: "blu",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "blu"},
								},
							},
						},
					},
				},
			},
			expectedError: true,
		},
		{
			name:       "no configmap, unused configmap",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "bla"},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:       "one configmap, required configmap, unused configmap",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "bla",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: "bla"},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:       "shared gpu configmaps",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Annotations: map[string]string{
						sharedGPUConfigMapNamePrefix: "shared-gpu-configmap",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Env: []v1.EnvVar{
								{
									Name: nvidiaVisibleDevices,
									ValueFrom: &v1.EnvVarSource{
										ConfigMapKeyRef: &v1.ConfigMapKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
												Name: "shared-gpu-configmap-0",
											},
										},
									},
								},
							},
							EnvFrom: []v1.EnvFromSource{
								{
									ConfigMapRef: &v1.ConfigMapEnvSource{
										LocalObjectReference: v1.LocalObjectReference{
											Name: "shared-gpu-configmap-evar-0",
										},
									},
								},
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "shared-gpu-configmap-vol",
								},
								{
									Name: "shared-gpu-configmap-evar-vol",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "shared-gpu-configmap-vol",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "shared-gpu-configmap-0",
									},
								},
							},
						},
						{
							Name: "shared-gpu-configmap-evar-vol",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "shared-gpu-configmap-evar-0",
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:       "env var referenced configmaps",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Annotations: map[string]string{
						sharedGPUConfigMapNamePrefix: "shared-gpu-configmap",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Env: []v1.EnvVar{
								{
									Name: nvidiaVisibleDevices,
									ValueFrom: &v1.EnvVarSource{
										ConfigMapKeyRef: &v1.ConfigMapKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
												Name: "cm-name",
											},
										},
									},
								},
							},
							EnvFrom: []v1.EnvFromSource{
								{
									ConfigMapRef: &v1.ConfigMapEnvSource{
										LocalObjectReference: v1.LocalObjectReference{
											Name: "cm-name2",
										},
									},
								},
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name: "cm-name-vol",
								},
								{
									Name: "cm-name-evar-vol",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "cm-name-vol",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "cm-name-0",
									},
								},
							},
						},
						{
							Name: "cm-name-evar-vol",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "cm-name-evar-0",
									},
								},
							},
						},
					},
				},
			},
			expectedError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmp := NewConfigMapPredicate(test.configMaps)
			_, status := cmp.PreFilter(context.Background(), nil, test.pod, nil)
			isError := status.AsError() != nil
			if isError != test.expectedError {
				t.Errorf("Test %s: Expected %t, got %v", test.name, test.expectedError, status)
			}
		})
	}
}

type FilterTest struct {
	name          string
	configMaps    map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo
	pod           *v1.Pod
	expectedError bool
}
