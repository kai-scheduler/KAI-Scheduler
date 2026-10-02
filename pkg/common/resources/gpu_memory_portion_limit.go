// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// CalcGpuMemoryPortionLimitAnnotationForContainer returns the kai.scheduler
// per-container GPU memory portion limit annotation key for containerName.
func CalcGpuMemoryPortionLimitAnnotationForContainer(containerName string) string {
	return constants.KaiFractionContainerAnnotationPrefix + containerName + constants.GpuMemoryPortionLimitSuffix
}

// ExtractGpuMemoryPortionLimitAnnotation returns the container name and raw
// value of the pod's gpu-memory.portion.limit annotation, if present.
func ExtractGpuMemoryPortionLimitAnnotation(pod *v1.Pod) (containerName string, rawValue string, found bool) {
	for annotationKey, annotationValue := range pod.Annotations {
		if !isGpuMemoryPortionLimitAnnotation(annotationKey) {
			continue
		}
		return gpuMemoryPortionLimitContainerName(annotationKey), annotationValue, true
	}
	return "", "", false
}

func isGpuMemoryPortionLimitAnnotation(annotationKey string) bool {
	return strings.HasPrefix(annotationKey, constants.KaiFractionContainerAnnotationPrefix) &&
		strings.HasSuffix(annotationKey, constants.GpuMemoryPortionLimitSuffix)
}

func gpuMemoryPortionLimitContainerName(annotationKey string) string {
	containerName := strings.TrimPrefix(annotationKey, constants.KaiFractionContainerAnnotationPrefix)
	return strings.TrimSuffix(containerName, constants.GpuMemoryPortionLimitSuffix)
}
