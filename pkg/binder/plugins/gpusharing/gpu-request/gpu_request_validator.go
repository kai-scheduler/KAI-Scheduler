// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpurequesthandler

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func ValidateGpuRequests(pod *v1.Pod) error {
	gpuFractionFromAnnotation, hasGpuFractionAnnotation := pod.Annotations[constants.GpuFraction]
	gpuMemoryFromAnnotation, hasGpuMemoryAnnotation := pod.Annotations[constants.GpuMemory]
	gpuFractionsCountFromAnnotation, hasGpuFractionsCount := pod.Annotations[constants.GpuFractionsNumDevices]

	mpsFromAnnotation, hasMpsAnnotation := pod.Annotations[constants.MpsAnnotation]

	wholeGPULimit := getFirstGPULimit(pod)
	hasWholeGPULimit := wholeGPULimit != nil

	isFractional := hasGpuFractionAnnotation || hasGpuMemoryAnnotation

	if !isFractional && hasMpsAnnotation && mpsFromAnnotation == "true" {
		return fmt.Errorf("MPS is only supported with GPU fraction request")
	}

	if hasGpuFractionAnnotation && hasWholeGPULimit {
		return fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit")
	}

	if hasGpuMemoryAnnotation && (hasGpuFractionAnnotation || hasWholeGPULimit) {
		return fmt.Errorf("cannot request both GPU and GPU memory")
	}

	if hasGpuFractionsCount && !(hasGpuFractionAnnotation || hasGpuMemoryAnnotation) {
		return fmt.Errorf(
			"cannot request multiple fractional devices without specifying fraction portion or gpu-memory",
		)
	}

	err := validateMemoryAnnotation(hasGpuMemoryAnnotation, gpuMemoryFromAnnotation)
	if err != nil {
		return err
	}

	err = validateGpuFractionAnnotation(hasGpuFractionAnnotation, gpuFractionFromAnnotation)
	if err != nil {
		return err
	}

	err = validateMultiFractionRequest(hasGpuFractionsCount, gpuFractionsCountFromAnnotation)
	if err != nil {
		return err
	}

	return nil
}

func validateMemoryAnnotation(hasGpuMemoryAnnotation bool, gpuMemoryFromAnnotation string) error {
	if !hasGpuMemoryAnnotation {
		return nil
	}
	gpuMemory, err := strconv.ParseUint(gpuMemoryFromAnnotation, 10, 64)
	if err != nil || gpuMemory == 0 {
		return fmt.Errorf("gpu-memory annotation value must be a positive integer greater than 0")
	}
	return nil
}

func validateGpuFractionAnnotation(hasGpuFractionAnnotation bool, gpuFractionFromAnnotation string) error {
	if !hasGpuFractionAnnotation {
		return nil
	}
	gpuFraction, gpuFractionErr := strconv.ParseFloat(gpuFractionFromAnnotation, 64)
	if gpuFractionErr != nil || gpuFraction <= 0 || gpuFraction >= 1 {
		return fmt.Errorf(
			"gpu-fraction annotation value must be a positive number smaller than 1.0")
	}
	return nil
}

func validateMultiFractionRequest(hasGpuFractionsCount bool, gpuFractionsCountFromAnnotation string) error {
	if !hasGpuFractionsCount {
		return nil
	}
	fractionsCount, err := strconv.ParseUint(gpuFractionsCountFromAnnotation, 10, 64)
	if err != nil || fractionsCount == 0 {
		return fmt.Errorf("fraction count annotation value must be a positive integer greater than 0")
	}
	return nil
}

// getFirstGPULimit gets the first GPU limit from the pod.Containers or pod.InitContainers.
func getFirstGPULimit(pod *v1.Pod) *resource.Quantity {
	containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
	for _, container := range containers {
		if limit, ok := container.Resources.Limits[constants.NvidiaGpuResource]; ok {
			return &limit
		}
	}
	return nil
}
