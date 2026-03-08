// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

var (
	fractionDevicesAnnotationNotFound = fmt.Errorf("num GPU fraction devices annotation not found")
)

func RequestsGPUFraction(pod *v1.Pod) bool {
	_, foundFraction := pod.Annotations[constants.GpuFraction]
	_, foundGPUMemory := pod.Annotations[constants.GpuMemory]
	return foundFraction || foundGPUMemory
}

func RequestsWholeGPU(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if _, ok := container.Resources.Requests[constants.NvidiaGpuResource]; ok {
			return true
		}
		if _, ok := container.Resources.Limits[constants.NvidiaGpuResource]; ok {
			return true
		}
	}
	return false
}

func RequestsGPU(pod *v1.Pod) bool {
	return RequestsGPUFraction(pod) || RequestsWholeGPU(pod)
}

func GetGPUFraction(pod *v1.Pod) (float64, error) {
	gpuFractionStr, found := pod.Annotations[constants.GpuFraction]
	if !found {
		return 0, fmt.Errorf("GPU fraction annotation not found")
	}
	fractionValue, err := strconv.ParseFloat(gpuFractionStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU fraction annotation value. err: %s", err)
	}
	return fractionValue, nil
}

func GetGPUMemory(pod *v1.Pod) (int64, error) {
	gpuMemoryStr, found := pod.Annotations[constants.GpuMemory]
	if !found {
		return 0, fmt.Errorf("GPU memory annotation not found")
	}
	memValue, err := strconv.ParseInt(gpuMemoryStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU memory annotation value. err: %s", err)
	}
	return memValue, nil
}

func GetNumGPUFractionDevices(pod *v1.Pod) (int64, error) {
	if pod.Annotations == nil {
		return 0, fractionDevicesAnnotationNotFound
	}
	mumDevicesStr, found := pod.Annotations[constants.GpuFractionsNumDevices]
	if !found {
		_, foundFraction := pod.Annotations[constants.GpuFraction]
		_, foundGPUMemory := pod.Annotations[constants.GpuMemory]
		if foundFraction || foundGPUMemory {
			return 1, nil
		}
		return 0, fractionDevicesAnnotationNotFound
	}
	mumDevicesValue, err := strconv.ParseInt(mumDevicesStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU fraction num devices annotation value. err: %s", err)
	}
	return mumDevicesValue, nil
}

func GetGpuGroups(pod *v1.Pod) []string {
	var gpuGroups []string
	gpuGroup, found := pod.Labels[constants.GPUGroup]
	if !found {
		return nil
	}
	gpuGroups = append(gpuGroups, gpuGroup)
	for labelKey, labelValue := range pod.Labels {
		if strings.HasPrefix(labelKey, constants.MultiGpuGroupLabelPrefix) {
			gpuGroups = append(gpuGroups, labelValue)
		}
	}
	return gpuGroups
}

func GetMultiFractionGpuGroupLabel(gpuGroup string) (string, string) {
	return constants.MultiGpuGroupLabelPrefix + gpuGroup, gpuGroup
}

func IsMultiFraction(pod *v1.Pod) (bool, error) {
	numDevices, err := GetNumGPUFractionDevices(pod)
	if err != nil {
		if errors.Is(err, fractionDevicesAnnotationNotFound) {
			return false, nil
		}
		return false, err
	}
	return numDevices > 1, nil
}
