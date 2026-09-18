// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type nvFractionsAnnotationType int

const (
	nvFractionsRequestAnnotation nvFractionsAnnotationType = iota
	nvFractionsLimitAnnotation
	nvFractionsDevicesAnnotation
	nvFractionsComputeModeAnnotation
	nvFractionsComputePortionAnnotation
	nvFractionsComputeRequestAnnotation
)

func CalcGpuFractionAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsMemoryRequestSuffix
}

func CalcGpuFractionLimitAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsMemoryLimitSuffix
}

func CalcGpuVisibleDevicesAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsVisibleDevicesSuffix
}

// CalcGpuComputePortionAnnotationForContainer returns the per-container GPU
// compute-portion annotation key.
func CalcGpuComputePortionAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsComputePortionSuffix
}

// CalcGpuComputeRequestAnnotationForContainer returns the per-container GPU
// compute-request annotation key.
func CalcGpuComputeRequestAnnotationForContainer(containerName string) string {
	return constants.NvFractionsAnnotationPrefix + containerName + constants.NvFractionsComputeRequestSuffix
}

func ExtractNvFractionsData(pod *v1.Pod) (map[string]NvFractionsContainerRequest, error) {
	fractionsData := make(map[string]NvFractionsContainerRequest)
	for annotationKey, annotationValue := range pod.Annotations {
		if !strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) {
			continue
		}
		if IsBinderOwnedNvFractionsAnnotation(annotationKey) {
			continue
		}

		containerName, annotationType, err := parseNvFractionsAnnotationKey(annotationKey)
		if err != nil {
			return nil, err
		}

		containerData := fractionsData[containerName]
		if annotationType == nvFractionsComputeModeAnnotation {
			if !IsValidGPUComputeSharingMode(annotationValue) {
				return nil, fmt.Errorf("invalid NvFractions compute mode: %s", annotationValue)
			}
			mode := schedulingv1alpha2.GPUComputeSharingMode(annotationValue)
			containerData.ComputeMode = &mode
			fractionsData[containerName] = containerData
			continue
		}
		if annotationType == nvFractionsComputeRequestAnnotation {
			computeRequest, err := parseNvFractionsComputeValue(annotationKey, annotationValue)
			if err != nil {
				return nil, err
			}
			containerData.ComputeRequest = &computeRequest
			fractionsData[containerName] = containerData
			continue
		}
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

// getNvFractionData returns the container request that carries the pod's
// NvFractions memory request, and the pod's GPU compute request. The compute
// request is returned separately because it is valid on its own: it refines any
// fraction source, including the legacy gpu-fraction and gpu-memory annotations.
func getNvFractionData(pod *v1.Pod) (*NvFractionsContainerRequest, float64, error) {
	nvFractionsData, err := ExtractNvFractionsData(pod)
	if err != nil {
		return nil, 0, err
	}

	var memoryRequest *NvFractionsContainerRequest
	computeRequest := float64(0)
	for _, containerData := range nvFractionsData {
		if memoryRequest == nil && containerData.Request != nil {
			containerDataCopy := containerData
			memoryRequest = &containerDataCopy
		}
		if computeRequest == 0 && containerData.ComputeRequest != nil {
			computeRequest = *containerData.ComputeRequest
		}
	}
	return memoryRequest, computeRequest, nil
}

// IsBinderOwnedNvFractionsAnnotation reports whether annotationKey is written by
// the binder after scheduling (device list, compute portion) rather than being
// part of the customer's request. Request parsing skips these instead of
// rejecting them as invalid keys, and admission only lets the binder set them.
func IsBinderOwnedNvFractionsAnnotation(annotationKey string) bool {
	if !strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) {
		return false
	}
	return strings.HasSuffix(annotationKey, constants.NvFractionsVisibleDevicesSuffix) ||
		strings.HasSuffix(annotationKey, constants.NvFractionsComputePortionSuffix)
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
	if strings.HasSuffix(annotationKey, constants.GpuComputeSharingModeSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.GpuComputeSharingModeSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsComputeModeAnnotation, nil
	}
	if strings.HasSuffix(annotationKey, constants.NvFractionsComputePortionSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.NvFractionsComputePortionSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsComputePortionAnnotation, nil
	}
	if strings.HasSuffix(annotationKey, constants.NvFractionsComputeRequestSuffix) {
		containerName := strings.TrimSuffix(containerNameWithSuffix, constants.NvFractionsComputeRequestSuffix)
		if containerName == "" {
			return "", 0, fmt.Errorf("invalid NvFractions annotation key: %s", annotationKey)
		}
		return containerName, nvFractionsComputeRequestAnnotation, nil
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

// parseNvFractionsComputeValue parses a GPU compute request, a share of a single
// device's compute expressed as a fraction in (0, 1].
func parseNvFractionsComputeValue(annotationKey, annotationValue string) (float64, error) {
	computeRequest, err := strconv.ParseFloat(annotationValue, 64)
	if err != nil || math.IsNaN(computeRequest) || computeRequest <= 0 || computeRequest > 1 {
		return 0, fmt.Errorf(
			"%s annotation value must be a positive number no greater than 1.0", annotationKey,
		)
	}
	return computeRequest, nil
}

func defaultRequestFromLimit(containerData *NvFractionsContainerRequest) {
	if containerData.Request != nil || containerData.Limit == nil {
		return
	}
	limitCopy := *containerData.Limit
	containerData.Request = &limitCopy
}
