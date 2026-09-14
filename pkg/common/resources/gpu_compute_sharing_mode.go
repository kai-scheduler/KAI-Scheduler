// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// CalcGpuComputeSharingModeAnnotationForContainer returns the per-container
// GPU compute-sharing-mode annotation key for containerName.
func CalcGpuComputeSharingModeAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.GpuComputeSharingModeSuffix
}

// ExtractGpuComputeSharingModeAnnotation returns the container name and raw
// value of the pod's gpu-compute.mode annotation, if present.
func ExtractGpuComputeSharingModeAnnotation(pod *v1.Pod) (containerName string, rawValue string, found bool) {
	for annotationKey, annotationValue := range pod.Annotations {
		if !isGpuComputeSharingModeAnnotation(annotationKey) {
			continue
		}
		return gpuComputeSharingModeContainerName(annotationKey), annotationValue, true
	}
	return "", "", false
}

func isGpuComputeSharingModeAnnotation(annotationKey string) bool {
	return strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) &&
		strings.HasSuffix(annotationKey, constants.GpuComputeSharingModeSuffix)
}

func gpuComputeSharingModeContainerName(annotationKey string) string {
	containerName := strings.TrimPrefix(annotationKey, constants.NvFractionsAnnotationPrefix)
	return strings.TrimSuffix(containerName, constants.GpuComputeSharingModeSuffix)
}
