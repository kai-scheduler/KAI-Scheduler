// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deviceaccess

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func cpuContainer(name string, env ...v1.EnvVar) v1.Container {
	return v1.Container{
		Name: name,
		Resources: v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("100m"),
			},
		},
		Env: env,
	}
}

func visibleDevicesEnv(value string) v1.EnvVar {
	return v1.EnvVar{Name: constants.NvidiaVisibleDevices, Value: value}
}

func visibleDevicesValueFromEnv() v1.EnvVar {
	return v1.EnvVar{
		Name: constants.NvidiaVisibleDevices,
		ValueFrom: &v1.EnvVarSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				LocalObjectReference: v1.LocalObjectReference{Name: "some-configmap"},
			},
		},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedErr string
	}{
		{
			name: "init container without NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0")},
			}},
		},
		{
			name: "init container with NVIDIA_VISIBLE_DEVICES=void env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesEnv("void"))},
			}},
		},
		{
			name: "init container with NVIDIA_VISIBLE_DEVICES=none env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesEnv("none"))},
			}},
		},
		{
			name: "init container with NVIDIA_VISIBLE_DEVICES=all env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesEnv("all"))},
			}},
			expectedErr: "container init-container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of all. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "init container with invalid single index NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesEnv("7"))},
			}},
			expectedErr: "container init-container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of 7. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "init container with invalid multi index NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesEnv("3,6"))},
			}},
			expectedErr: "container init-container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of 3,6. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "init container with NVIDIA_VISIBLE_DEVICES env var mounted from config map",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0", visibleDevicesValueFromEnv())},
			}},
			expectedErr: "container init-container-0 has an environment variable NVIDIA_VISIBLE_DEVICES defined " +
				"with a valueFrom reference. This is forbidden due to possible conflicts with Nvidia's device plugin",
		},
		{
			name: "container without NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0")},
			}},
		},
		{
			name: "container with NVIDIA_VISIBLE_DEVICES=void env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("void"))},
			}},
		},
		{
			name: "container with NVIDIA_VISIBLE_DEVICES=none env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("none"))},
			}},
		},
		{
			name: "container with NVIDIA_VISIBLE_DEVICES=all env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("all"))},
			}},
			expectedErr: "container container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of all. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "container with invalid single index NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("7"))},
			}},
			expectedErr: "container container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of 7. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "container with invalid multi index NVIDIA_VISIBLE_DEVICES env var",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("3,6"))},
			}},
			expectedErr: "container container-0 has an environment variable NVIDIA_VISIBLE_DEVICES" +
				" defined with a value of 3,6. This is forbidden due to conflicts with Nvidia's device plugin." +
				" The only values that are allowed are 'void' or 'none'",
		},
		{
			name: "container with NVIDIA_VISIBLE_DEVICES env var mounted from config map",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesValueFromEnv())},
			}},
			expectedErr: "container container-0 has an environment variable NVIDIA_VISIBLE_DEVICES defined " +
				"with a valueFrom reference. This is forbidden due to possible conflicts with Nvidia's device plugin",
		},
	}

	plugin := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.Validate(tt.pod)
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.expectedErr)
			}
		})
	}
}
