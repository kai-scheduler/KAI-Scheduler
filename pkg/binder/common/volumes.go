// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"regexp"
	"strings"

	"k8s.io/api/core/v1"
)

func SetConfigMapVolume(pod *v1.Pod, configMapName string) {
	volumeName := GetConfigVolumeName(configMapName)
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

func GetConfigVolumeName(configMapName string) string {
	// ConfigMap names may be DNS subdomains, but volume names must be DNS labels.
	volumeName := strings.ToLower(configMapName + "-vol")
	volumeName = invalidDNSLabelChars.ReplaceAllString(volumeName, "-")
	return strings.Trim(volumeName, "-")
}
