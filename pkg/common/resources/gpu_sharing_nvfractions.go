// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type nvFractionsAnnotationType int

const (
	nvFractionsRequestAnnotation nvFractionsAnnotationType = iota
	nvFractionsLimitAnnotation
	nvFractionsDevicesAnnotation
)

func CalcGpuFractionAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsMemoryRequestSuffix
}

func CalcGpuVisibleDevicesAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsVisibleDevicesSuffix
}

func ExtractNvFractionsData(pod *v1.Pod) (map[string]NvFractionsContainerRequest, error) {
	fractionsData := make(map[string]NvFractionsContainerRequest)
	for annotationKey, annotationValue := range pod.Annotations {
		if !strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) {
			continue
		}

		containerName, annotationType, err := parseNvFractionsAnnotationKey(annotationKey)
		if err != nil {
			return nil, err
		}

		containerData := fractionsData[containerName]
		switch annotationType {
		case nvFractionsRequestAnnotation:
			gpuMemory, err := parseNvFractionsAnnotationValue(annotationKey, annotationValue)
			if err != nil {
				return nil, err
			}
			containerData.Request = &gpuMemory
		case nvFractionsLimitAnnotation:
			gpuMemory, err := parseNvFractionsAnnotationValue(annotationKey, annotationValue)
			if err != nil {
				return nil, err
			}
			containerData.Limit = &gpuMemory
		}

		defaultRequestFromLimit(&containerData)
		fractionsData[containerName] = containerData
	}
	return fractionsData, nil
}

func getNvFractionData(pod *v1.Pod) (*NvFractionsContainerRequest, error) {
	nvFractionsData, err := ExtractNvFractionsData(pod)
	if err != nil {
		return nil, err
	}
	for _, containerData := range nvFractionsData {
		if containerData.Request != nil {
			return &containerData, nil
		}
	}
	return nil, nil
}

func parseNvFractionsAnnotationKey(annotationKey string) (string, nvFractionsAnnotationType, error) {
	containerNameWithSuffix := strings.TrimPrefix(annotationKey, constants.NvFractionsAnnotationPrefix)
	if strings.HasSuffix(annotationKey, constants.NvFractionsMemoryRequestSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.NvFractionsMemoryRequestSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsRequestAnnotation, nil
	}
	if strings.HasSuffix(annotationKey, constants.NvFractionsMemoryLimitSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.NvFractionsMemoryLimitSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsLimitAnnotation, nil
	}
	if strings.HasSuffix(annotationKey, constants.NvFractionsVisibleDevicesSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.NvFractionsVisibleDevicesSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsDevicesAnnotation, nil
	}
	return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
}

func parseNvFractionsAnnotationValue(annotationKey, annotationValue string) (resource.Quantity, error) {
	gpuMemory, err := resource.ParseQuantity(annotationValue)
	if err != nil || gpuMemory.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf(
			"%s annotation value must be a valid Kubernetes memory quantity greater than 0", annotationKey,
		)
	}
	return gpuMemory, nil
}

func defaultRequestFromLimit(containerData *NvFractionsContainerRequest) {
	if containerData.Request != nil || containerData.Limit == nil {
		return
	}
	limitCopy := *containerData.Limit
	containerData.Request = &limitCopy
}
