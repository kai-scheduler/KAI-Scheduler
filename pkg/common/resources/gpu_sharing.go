// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type FractionType string

const (
	BytesInMiB int64 = 1024 * 1024

	FractionTypeNvFractions FractionType = "nv-fractions"
	FractionTypePortion     FractionType = "portion"
	FractionTypeMemory      FractionType = "memory"
)

// PodGPUFractionRequest is the resolved GPU fraction request for a pod.
// Priority is pre-applied: NvFractions request annotation wins over gpu-fraction and gpu-memory.
type PodGPUFractionRequest struct {
	// Portion is the effective GPU fraction (0-1).
	// Non-zero only when no NvFractions request annotation is present.
	Portion float64
	// Memory is the effective GPU memory request.
	// Set when NvFractions or gpu-memory annotation is the effective source.
	Memory *resource.Quantity
	// Limit is the effective GPU memory limit.
	// Set when NvFractions limit annotation is present.
	Limit *resource.Quantity
	// NumDevices is the number of fractional GPU devices (>= 1).
	NumDevices int64

	FractionType FractionType
}

type NvFractionsContainerRequest struct {
	Request     *resource.Quantity
	Limit       *resource.Quantity
	ComputeMode *schedulingv1alpha2.GPUComputeSharingMode
}

var (
	fractionDevicesAnnotationNotFound = fmt.Errorf("num GPU fraction devices annotation not found")
)

func RequestsGPU(pod *v1.Pod) bool {
	return RequestsGPUFraction(pod) || RequestsWholeGPU(pod)
}

func RequestsGPUFraction(pod *v1.Pod) bool {
	_, foundFraction := pod.Annotations[constants.GpuFraction]
	_, foundGPUMemory := pod.Annotations[constants.GpuMemory]
	if foundFraction || foundGPUMemory {
		return true
	}
	for annotationKey := range pod.Annotations {
		if strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) {
			// A request or a limit annotation both mark a fractional request: a
			// limit-only pod has its request defaulted from the limit (see
			// ParsePodGPUFractionRequest), so it must be recognized here too.
			if strings.HasSuffix(annotationKey, constants.NvFractionsMemoryRequestSuffix) ||
				strings.HasSuffix(annotationKey, constants.NvFractionsMemoryLimitSuffix) {
				return true
			}
		}
	}
	return false
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

func GetNumGPUFractionDevices(pod *v1.Pod) (int64, error) {
	req, err := ParsePodGPUFractionRequest(pod)
	if err != nil {
		return 0, err
	}
	if req == nil {
		return 0, fractionDevicesAnnotationNotFound
	}
	return req.NumDevices, nil
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

// ParsePodGPUFractionRequest parses and resolves all GPU fraction annotations from the pod.
// Returns nil if the pod has no GPU fraction annotations.
// All present annotation values are validated; an error is returned if any value is invalid.
// NvFractions request takes priority over gpu-fraction and gpu-memory annotations.
func ParsePodGPUFractionRequest(pod *v1.Pod) (*PodGPUFractionRequest, error) {
	numDevices, err := parseNumGPUFractionDevices(pod)
	if err != nil {
		return nil, err
	}

	nvFractions, err := getNvFractionData(pod)
	if err != nil {
		return nil, err
	}
	if nvFractions != nil {
		return &PodGPUFractionRequest{
			Memory:       nvFractions.Request,
			Limit:        nvFractions.Limit,
			NumDevices:   numDevices,
			FractionType: FractionTypeNvFractions,
		}, nil
	}

	if portion, found, err := parseGpuFractionalPortion(pod); found || err != nil {
		if err != nil {
			return nil, err
		}
		return &PodGPUFractionRequest{
			Portion:      portion,
			NumDevices:   numDevices,
			FractionType: FractionTypePortion,
		}, nil
	}

	if memory, found, err := parseGpuFractionalMemory(pod); found || err != nil {
		if err != nil {
			return nil, err
		}
		return &PodGPUFractionRequest{
			Memory:       memory,
			NumDevices:   numDevices,
			FractionType: FractionTypeMemory,
		}, nil
	}

	return nil, nil
}

func parseNumGPUFractionDevices(pod *v1.Pod) (int64, error) {
	numDevicesStr, hasNumDevices := pod.Annotations[constants.GpuFractionsNumDevices]
	if !hasNumDevices {
		return 1, nil
	}
	numDevices, err := strconv.ParseInt(numDevicesStr, 10, 64)
	if err != nil || numDevices <= 0 {
		return 0, fmt.Errorf("fraction count annotation value must be a positive integer greater than 0")
	}
	return numDevices, nil
}

func parseGpuFractionalPortion(pod *v1.Pod) (float64, bool, error) {
	gpuFractionPortionStr, hasLegacyFraction := pod.Annotations[constants.GpuFraction]
	if !hasLegacyFraction || gpuFractionPortionStr == "" {
		return 0, false, nil
	}

	portion, err := strconv.ParseFloat(gpuFractionPortionStr, 64)
	if err != nil || portion <= 0 || portion >= 1 {
		return 0, true, fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0")
	}
	return portion, true, nil
}

func parseGpuFractionalMemory(pod *v1.Pod) (*resource.Quantity, bool, error) {
	gpuMemoryStr, hasLegacyMemory := pod.Annotations[constants.GpuMemory]
	if !hasLegacyMemory || gpuMemoryStr == "" {
		return nil, false, nil
	}

	memoryMiB, err := strconv.ParseUint(gpuMemoryStr, 10, 64)
	if err != nil || memoryMiB == 0 {
		return nil, true, fmt.Errorf("gpu-memory annotation value must be a positive integer greater than 0")
	}
	return GpuMemoryAnnotationToNvFractionsMemoryRequest(memoryMiB), true, nil
}

func GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemory uint64) *resource.Quantity {
	memory := resource.MustParse(fmt.Sprintf("%dMi", gpuMemory))
	return &memory
}

func IsValidGPUComputeSharingMode(mode string) bool {
	return mode == string(schedulingv1alpha2.GPUComputeSharingModeTimeSlicing) ||
		mode == string(schedulingv1alpha2.GPUComputeSharingModeSMSharing)
}
