// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deviceaccess

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func gpuContainer(name string) v1.Container {
	return v1.Container{
		Name: name,
		Resources: v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceName(constants.NvidiaGpuResource): resource.MustParse("1"),
			},
		},
	}
}

func migContainer(name string) v1.Container {
	return v1.Container{
		Name: name,
		Resources: v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				"nvidia.com/mig-3g.20gb": resource.MustParse("1"),
			},
		},
	}
}

func fractionPod(initContainers, containers []v1.Container) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "n1",
			Annotations: map[string]string{constants.GpuFraction: "0.5"},
		},
		Spec: v1.PodSpec{InitContainers: initContainers, Containers: containers},
	}
}

// voidedContainerNames returns the names of containers that have
// NVIDIA_VISIBLE_DEVICES set to "void" (i.e. were blocked from GPU access).
func voidedContainerNames(pod *v1.Pod) []string {
	var names []string
	for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		for _, env := range container.Env {
			if env.Name == constants.NvidiaVisibleDevices && env.Value == "void" {
				names = append(names, container.Name)
				break
			}
		}
	}
	return names
}

func TestMutate(t *testing.T) {
	tests := []struct {
		name    string
		pod     *v1.Pod
		blocked []string // containers expected to get NVIDIA_VISIBLE_DEVICES=void
	}{
		{
			name: "CPU pod - all containers blocked",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0")},
				Containers:     []v1.Container{cpuContainer("container-0"), cpuContainer("container-1")},
			}},
			blocked: []string{"init-container-0", "container-0", "container-1"},
		},
		{
			name: "CPU container with a pre-existing NVIDIA_VISIBLE_DEVICES is overridden to void",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{cpuContainer("container-0", visibleDevicesEnv("7"))},
			}},
			blocked: []string{"container-0"},
		},
		{
			name: "whole GPU pod - single GPU container not blocked",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{gpuContainer("container-0")},
			}},
			blocked: nil,
		},
		{
			name: "whole GPU pod - multiple GPU containers not blocked",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{gpuContainer("container-0"), gpuContainer("container-1")},
			}},
			blocked: nil,
		},
		{
			name: "whole GPU pod - CPU sidecar blocked, GPU container not",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{gpuContainer("container-0"), cpuContainer("container-1")},
			}},
			blocked: []string{"container-1"},
		},
		{
			name: "whole GPU pod - CPU init container blocked, GPU container not",
			pod: &v1.Pod{Spec: v1.PodSpec{
				InitContainers: []v1.Container{cpuContainer("init-container-0")},
				Containers:     []v1.Container{gpuContainer("container-0")},
			}},
			blocked: []string{"init-container-0"},
		},
		{
			name:    "fractional GPU pod - fraction container exempt",
			pod:     fractionPod(nil, []v1.Container{cpuContainer("container-0")}),
			blocked: nil,
		},
		{
			name:    "fractional GPU pod - sidecar blocked, fraction container exempt",
			pod:     fractionPod(nil, []v1.Container{cpuContainer("container-0"), cpuContainer("container-1")}),
			blocked: []string{"container-1"},
		},
		{
			name:    "fractional GPU pod - init container blocked, fraction container exempt",
			pod:     fractionPod([]v1.Container{cpuContainer("init-container-0")}, []v1.Container{cpuContainer("container-0")}),
			blocked: []string{"init-container-0"},
		},
		{
			name: "pod requesting a MIG device not blocked",
			pod: &v1.Pod{Spec: v1.PodSpec{
				Containers: []v1.Container{migContainer("container-0")},
			}},
			blocked: nil,
		},
	}

	plugin := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, plugin.Mutate(tt.pod))
			assert.ElementsMatch(t, tt.blocked, voidedContainerNames(tt.pod))
		})
	}
}

// TestFractionPodWithoutRegularContainers guards against a panic: a pod with a GPU-fraction
// annotation but no regular containers can reach the mutating webhook before the API server
// enforces containers >= 1. GetFractionContainerRef indexes pod.Spec.Containers[0], so the
// plugin must short-circuit instead of panicking.
func TestFractionPodWithoutRegularContainers(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{constants.GpuFraction: "0.5"}},
		Spec:       v1.PodSpec{InitContainers: []v1.Container{cpuContainer("init-container-0")}},
	}
	plugin := New()
	assert.NoError(t, plugin.Validate(pod))
	assert.NoError(t, plugin.Mutate(pod))
}
