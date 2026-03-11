// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingconfigmap

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type configMapNameTest struct {
	pod                  *v1.Pod
	containerIndex       int
	desiredConfigMapName string
	expectedError        bool
}

func TestGetDesiredConfigMapName(t *testing.T) {
	tests := []configMapNameTest{
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"runai-job-name": "job1",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{},
					},
				},
			},
			containerIndex:       0,
			desiredConfigMapName: "",
			expectedError:        true,
		},
		{
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						gpuSharingConfigMapAnnotation: "config-map-name",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{},
						{},
					},
					Volumes: []v1.Volume{
						{
							Name: "config-map-vol",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "config-map-name-1",
									},
								},
							},
						},
					}},
			},
			containerIndex:       1,
			desiredConfigMapName: "config-map-name-1",
			expectedError:        false,
		},
	}
	tests = []configMapNameTest{tests[1]}
	for _, test := range tests {
		containerRef := &PodContainerRef{
			Container: &test.pod.Spec.Containers[test.containerIndex],
			Index:     test.containerIndex,
			Type:      RegularContainer,
		}
		configMapName, err := ExtractCapabilitiesConfigMapName(test.pod, containerRef)
		if test.expectedError && err == nil {
			t.Errorf("Expected error but got none")
		}
		if !test.expectedError && err != nil {
			t.Errorf("Expected no error but got %v", err)
		}
		if configMapName != test.desiredConfigMapName {
			t.Errorf("Expected config map name %v but got %v", test.desiredConfigMapName, configMapName)
		}
	}
}
