// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestAddConfigMapVolume(t *testing.T) {
	podSpec := &v1.PodSpec{}
	addConfigMapVolume(podSpec, "vol", "conf")
	assert.Equal(t, len(podSpec.Volumes), 1)
	assert.Equal(t, podSpec.Volumes[0].Name, "vol")
	assert.Equal(t, podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name, "conf")
}

func TestOverrideConfigMapVolume(t *testing.T) {
	podSpec := &v1.PodSpec{
		Volumes: []v1.Volume{
			{
				Name: "name",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "conf1",
						},
					},
				},
			},
		},
	}

	addConfigMapVolume(podSpec, "name", "conf2")
	assert.Equal(t, len(podSpec.Volumes), 1)
	assert.Equal(t, podSpec.Volumes[0].Name, "name")
	assert.Equal(t, podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name, "conf2")
}

func TestGetConfigVolumeName(t *testing.T) {
	tests := []struct {
		name          string
		configMapName string
		expected      string
	}{
		{
			name:          "replaces dots and keeps suffix",
			configMapName: "test.pod-abc1234-shared-gpu-0",
			expected:      "test-pod-abc1234-shared-gpu-0-vol",
		},
		{
			name:          "normalizes unsupported characters",
			configMapName: "test_pod.config@1",
			expected:      "test-pod-config-1-vol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := GetConfigVolumeName(tt.configMapName)

			assert.Equal(t, tt.expected, actual)
			assert.True(t, strings.HasSuffix(actual, "-vol"))
			assert.NotContains(t, actual, ".")
			assert.Empty(t, validation.IsDNS1123Label(actual))
		})
	}
}
