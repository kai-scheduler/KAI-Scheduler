// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/configmap_info"
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
						sharedGPUConfigMapAnnotation: "shared-gpu-configmap",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Env: []v1.EnvVar{
								{
									Name: constants.NvidiaVisibleDevices,
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
						sharedGPUConfigMapAnnotation: "shared-gpu-configmap",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Env: []v1.EnvVar{
								{
									Name: constants.NvidiaVisibleDevices,
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

func TestPreFilterOptionalConfigMapVolumes(t *testing.T) {
	for _, test := range []FilterTest{
		{
			name:          "optional configmap volume, missing",
			configMaps:    nil,
			pod:           podWithConfigMapVolume("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required configmap volume (Optional=false), missing",
			configMaps:    nil,
			pod:           podWithConfigMapVolume("required-cm", boolPtr(false)),
			expectedError: true,
		},
		{
			name:       "mixed optional and required volumes, required missing",
			configMaps: map[common_info.ConfigMapID]*configmap_info.ConfigMapInfo{},
			pod: podWithConfigMapVolumes(
				configMapVolumeSpec{name: "optional-cm", optional: boolPtr(true)},
				configMapVolumeSpec{name: "required-cm", optional: boolPtr(false)},
			),
			expectedError: true,
		},
		{
			name:       "projected configmap volume not mounted",
			configMaps: nil,
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{}},
					Volumes: []v1.Volume{
						{
							Name: "projected-cm",
							VolumeSource: v1.VolumeSource{
								Projected: &v1.ProjectedVolumeSource{
									Sources: []v1.VolumeProjection{
										{
											ConfigMap: &v1.ConfigMapProjection{
												LocalObjectReference: v1.LocalObjectReference{Name: "required-cm"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
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

func TestPreFilterOptionalConfigMapProjectedVolumes(t *testing.T) {
	for _, test := range []FilterTest{
		{
			name:          "optional projected configmap volume, missing",
			configMaps:    nil,
			pod:           podWithProjectedConfigMapVolume("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required projected configmap volume, missing",
			configMaps:    nil,
			pod:           podWithProjectedConfigMapVolume("required-cm", boolPtr(false)),
			expectedError: true,
		},
		{
			name:       "projected volume with mixed configmaps, required missing",
			configMaps: nil,
			pod: podWithMultipleProjectedConfigMaps(
				projectedConfigMapSpec{name: "optional-cm", optional: boolPtr(true)},
				projectedConfigMapSpec{name: "required-cm", optional: boolPtr(false)},
			),
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

func TestPreFilterOptionalConfigMapEnvVars(t *testing.T) {
	for _, test := range []FilterTest{
		{
			name:          "optional configmap env var, missing",
			configMaps:    nil,
			pod:           podWithConfigMapEnvVar("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required configmap env var (Optional=false), missing",
			configMaps:    nil,
			pod:           podWithConfigMapEnvVar("required-cm", boolPtr(false)),
			expectedError: true,
		},
		{
			name:          "optional configmap envFrom, missing",
			configMaps:    nil,
			pod:           podWithConfigMapEnvFrom("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required configmap envFrom (Optional=false), missing",
			configMaps:    nil,
			pod:           podWithConfigMapEnvFrom("required-cm", boolPtr(false)),
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

func TestPreFilterOptionalConfigMapEphemeralEnvVars(t *testing.T) {
	for _, test := range []FilterTest{
		{
			name:          "optional configmap env var in ephemeral container, missing",
			configMaps:    nil,
			pod:           podWithEphemeralConfigMapEnvVar("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required configmap env var in ephemeral container, missing",
			configMaps:    nil,
			pod:           podWithEphemeralConfigMapEnvVar("required-cm", boolPtr(false)),
			expectedError: true,
		},
		{
			name:          "optional configmap envFrom in ephemeral container, missing",
			configMaps:    nil,
			pod:           podWithEphemeralConfigMapEnvFrom("optional-cm", boolPtr(true)),
			expectedError: false,
		},
		{
			name:          "required configmap envFrom in ephemeral container, missing",
			configMaps:    nil,
			pod:           podWithEphemeralConfigMapEnvFrom("required-cm", boolPtr(false)),
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

func boolPtr(b bool) *bool {
	return &b
}

type configMapVolumeSpec struct {
	name     string
	optional *bool
}

type projectedConfigMapSpec struct {
	name     string
	optional *bool
}

func podWithConfigMapVolume(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				VolumeMounts: []v1.VolumeMount{{Name: cmName}},
			}},
			Volumes: []v1.Volume{{
				Name: cmName,
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{Name: cmName},
						Optional:             optional,
					},
				},
			}},
		},
	}
}

func podWithConfigMapVolumes(specs ...configMapVolumeSpec) *v1.Pod {
	var volumeMounts []v1.VolumeMount
	var volumes []v1.Volume
	for _, spec := range specs {
		volumeMounts = append(volumeMounts, v1.VolumeMount{Name: spec.name})
		volumes = append(volumes, v1.Volume{
			Name: spec.name,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{Name: spec.name},
					Optional:             spec.optional,
				},
			},
		})
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{VolumeMounts: volumeMounts}},
			Volumes:    volumes,
		},
	}
}

func podWithProjectedConfigMapVolume(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				VolumeMounts: []v1.VolumeMount{{Name: "projected-cm"}},
			}},
			Volumes: []v1.Volume{{
				Name: "projected-cm",
				VolumeSource: v1.VolumeSource{
					Projected: &v1.ProjectedVolumeSource{
						Sources: []v1.VolumeProjection{{
							ConfigMap: &v1.ConfigMapProjection{
								LocalObjectReference: v1.LocalObjectReference{Name: cmName},
								Optional:             optional,
							},
						}},
					},
				},
			}},
		},
	}
}

func podWithMultipleProjectedConfigMaps(specs ...projectedConfigMapSpec) *v1.Pod {
	var sources []v1.VolumeProjection
	for _, spec := range specs {
		sources = append(sources, v1.VolumeProjection{
			ConfigMap: &v1.ConfigMapProjection{
				LocalObjectReference: v1.LocalObjectReference{Name: spec.name},
				Optional:             spec.optional,
			},
		})
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				VolumeMounts: []v1.VolumeMount{{Name: "projected-cm"}},
			}},
			Volumes: []v1.Volume{{
				Name: "projected-cm",
				VolumeSource: v1.VolumeSource{
					Projected: &v1.ProjectedVolumeSource{
						Sources: sources,
					},
				},
			}},
		},
	}
}

func podWithConfigMapEnvVar(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Env: []v1.EnvVar{{
					Name: "VAR",
					ValueFrom: &v1.EnvVarSource{
						ConfigMapKeyRef: &v1.ConfigMapKeySelector{
							LocalObjectReference: v1.LocalObjectReference{Name: cmName},
							Key:                  "key",
							Optional:             optional,
						},
					},
				}},
			}},
		},
	}
}

func podWithConfigMapEnvFrom(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				EnvFrom: []v1.EnvFromSource{{
					ConfigMapRef: &v1.ConfigMapEnvSource{
						LocalObjectReference: v1.LocalObjectReference{Name: cmName},
						Optional:             optional,
					},
				}},
			}},
		},
	}
}

func podWithEphemeralConfigMapEnvVar(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			EphemeralContainers: []v1.EphemeralContainer{{
				EphemeralContainerCommon: v1.EphemeralContainerCommon{
					Env: []v1.EnvVar{{
						Name: "VAR",
						ValueFrom: &v1.EnvVarSource{
							ConfigMapKeyRef: &v1.ConfigMapKeySelector{
								LocalObjectReference: v1.LocalObjectReference{Name: cmName},
								Key:                  "key",
								Optional:             optional,
							},
						},
					}},
				},
			}},
		},
	}
}

func podWithEphemeralConfigMapEnvFrom(cmName string, optional *bool) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Spec: v1.PodSpec{
			EphemeralContainers: []v1.EphemeralContainer{{
				EphemeralContainerCommon: v1.EphemeralContainerCommon{
					EnvFrom: []v1.EnvFromSource{{
						ConfigMapRef: &v1.ConfigMapEnvSource{
							LocalObjectReference: v1.LocalObjectReference{Name: cmName},
							Optional:             optional,
						},
					}},
				},
			}},
		},
	}
}
