// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"regexp"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const numOfGpusEnvVarBC = "RUNAI_NUM_OF_GPUS" // Deprecated, please use GPU_PORTION env var instead.

func addGPUSharingEnvVars(
	container *v1.Container, sharedGpuConfigMapName string, includeNvidiaVisibleDevices bool,
) {
	if includeNvidiaVisibleDevices {
		common.AddEnvVarToContainer(container, v1.EnvVar{
			Name: constants.NvidiaVisibleDevices,
			ValueFrom: &v1.EnvVarSource{
				ConfigMapKeyRef: &v1.ConfigMapKeySelector{
					Key: constants.NvidiaVisibleDevices,
					LocalObjectReference: v1.LocalObjectReference{
						Name: sharedGpuConfigMapName,
					},
				},
			},
		})
	}

	common.AddEnvVarToContainer(container, v1.EnvVar{
		Name: numOfGpusEnvVarBC,
		ValueFrom: &v1.EnvVarSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				Key: numOfGpusEnvVarBC,
				LocalObjectReference: v1.LocalObjectReference{
					Name: sharedGpuConfigMapName,
				},
			},
		},
	})

	common.AddEnvVarToContainer(container, v1.EnvVar{
		Name: common.GPUPortion,
		ValueFrom: &v1.EnvVarSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				Key: common.GPUPortion,
				LocalObjectReference: v1.LocalObjectReference{
					Name: sharedGpuConfigMapName,
				},
			},
		},
	})
}

func addDirectEnvVarsConfigMapSource(container *v1.Container, directEnvVarsMapName string) {
	for _, env := range container.EnvFrom {
		if env.ConfigMapRef != nil && env.ConfigMapRef.Name == directEnvVarsMapName {
			return
		}
	}

	container.EnvFrom = append(container.EnvFrom, v1.EnvFromSource{
		ConfigMapRef: &v1.ConfigMapEnvSource{
			LocalObjectReference: v1.LocalObjectReference{
				Name: directEnvVarsMapName,
			},
			Optional: ptr.To(false),
		},
	})
}

func setConfigMapVolume(pod *v1.Pod, configMapName string) {
	volumeName := getConfigVolumeName(configMapName)
	addConfigMapVolume(&pod.Spec, volumeName, configMapName)
}

func addConfigMapVolume(podSpec *v1.PodSpec, volumeName string, configMapName string) {
	if podSpec.Volumes == nil {
		podSpec.Volumes = make([]v1.Volume, 0)
	}

	updatedVolumes := make([]v1.Volume, 0)
	for _, volume := range podSpec.Volumes {
		if volume.Name != volumeName {
			updatedVolumes = append(updatedVolumes, volume)
		}
	}
	podSpec.Volumes = updatedVolumes

	volume := v1.Volume{
		Name: volumeName,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}
	podSpec.Volumes = append(podSpec.Volumes, volume)
}

var invalidDNSLabelChars = regexp.MustCompile(`[^a-z0-9-]+`)

func getConfigVolumeName(configMapName string) string {
	volumeName := strings.ToLower(configMapName + "-vol")
	volumeName = invalidDNSLabelChars.ReplaceAllString(volumeName, "-")
	return strings.Trim(volumeName, "-")
}
