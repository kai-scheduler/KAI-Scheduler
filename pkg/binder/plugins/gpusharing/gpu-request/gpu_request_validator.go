// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpurequesthandler

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func ValidateGpuRequests(pod *v1.Pod) error {
	containerIndex := getGpuSharingContainerIndex(pod.Labels, pod.Spec.Containers)
	container := pod.Spec.Containers[containerIndex]

	gpuFractionFromAnnotation, hasGpuFractionAnnotation := pod.Annotations[constants.GpuFraction]
	gpuMemoryFromAnnotation, hasGpuMemoryAnnotation := pod.Annotations[constants.GpuMemory]
	gpuFractionsCountFromAnnotation, hasGpuFractionsCount := pod.Annotations[constants.GpuFractionsNumDevices]

	mpsFromAnnotation, hasMpsAnnotation := pod.Annotations[constants.MpsAnnotation]
	_, hasGpuResourceRequest := container.Resources.Requests[constants.GpuResource]
	gpuLimitQuantity, hasGpuResourceLimit := container.Resources.Limits[constants.GpuResource]

	wholeGPULimit, hasWholeGPULimit := getWholeGpuLimit(hasGpuResourceLimit, gpuLimitQuantity)
	if hasWholeGPULimit && wholeGPULimit < 0 {
		return fmt.Errorf("GPU resource limit cannot be negative")
	}

	isFractional := isFractionalRequest(hasGpuResourceLimit, gpuLimitQuantity, hasGpuFractionAnnotation,
		hasGpuMemoryAnnotation)

	if !isFractional && hasMpsAnnotation && mpsFromAnnotation == "true" {
		return fmt.Errorf("MPS is only supported with GPU fraction request")
	}

	if hasGpuFractionAnnotation && hasGpuResourceLimit {
		return fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit")
	}

	if hasGpuResourceRequest && !hasGpuResourceLimit {
		return fmt.Errorf("GPU is not not listed in resource limits")
	}

	if hasGpuResourceLimit && !isGpuFractionValid(hasWholeGPULimit, gpuLimitQuantity) {
		return fmt.Errorf("GPU resource limit annotation cannot be larger than 1")
	}

	if hasGpuMemoryAnnotation && (hasGpuFractionAnnotation || hasGpuResourceLimit) {
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

func getGpuSharingContainerIndex(labels map[string]string, containers []v1.Container) int {
	if len(containers) == 1 {
		return 0
	}

	for label := range labels {
		if strings.HasPrefix(label, "pipelines.kubeflow.org") {
			return 1
		}
	}

	return 0
}

func isFractionalRequest(hasGpuResourceLimit bool, gpuResourceLimit resource.Quantity,
	hasGpuFractionAnnotation bool, hasGpuMemoryAnnotation bool) bool {
	_, hasWholeGpuLimit := getWholeGpuLimit(hasGpuResourceLimit, gpuResourceLimit)
	hasFractionalGPULimit := hasGpuResourceLimit && !hasWholeGpuLimit
	isFractional := hasGpuFractionAnnotation || hasGpuMemoryAnnotation || hasFractionalGPULimit
	return isFractional
}

func isGpuFractionValid(hasWholeGPULimit bool, gpuLimitQuantity resource.Quantity) bool {
	return hasWholeGPULimit || gpuLimitQuantity.MilliValue() <= 1000
}

func getWholeGpuLimit(hasGpuResourceLimit bool, gpuResourceLimit resource.Quantity) (int64, bool) {
	var wholeGPULimit int64
	var hasWholeGPULimit bool
	if hasGpuResourceLimit {
		wholeGPULimit, hasWholeGPULimit = gpuResourceLimit.AsInt64()
	}
	return wholeGPULimit, hasWholeGPULimit
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
