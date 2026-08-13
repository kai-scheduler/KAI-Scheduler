// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

// ValidateGPUFractionRequest validates a pod's GPU fraction request across all
// supported annotation forms: legacy gpu-fraction (portion), legacy gpu-memory,
// the gpu-fraction-container-name annotation, and NvFractions annotations. It is
// configmap-agnostic and shared by the admission plugins.
func ValidateGPUFractionRequest(pod *v1.Pod) error {
	if err := validateGpuMemoryPortionLimitAnnotation(pod); err != nil {
		return err
	}

	req, err := ParsePodGPUFractionRequest(pod)
	if err != nil {
		return err
	}
	if req == nil {
		if _, hasCount := pod.Annotations[constants.GpuFractionsNumDevices]; hasCount {
			return fmt.Errorf("cannot request multiple fractional devices without specifying fraction details (portion or memory)")
		}
		return nil
	}

	hasNvFractionalAnnotations := req.FractionType == FractionTypeNvFractions

	// Structural NvFractions validation: container references, request ≤ limit, single container.
	if err := validateNvFractionsAnnotations(hasNvFractionalAnnotations, pod); err != nil {
		return err
	}

	if getFirstGPULimit(pod) != nil {
		return fmt.Errorf("cannot have both GPU fraction request and whole GPU resource request/limit")
	}

	legacyFractionStr, hasLegacyFraction := pod.Annotations[constants.GpuFraction]
	hasLegacyFraction = hasLegacyFraction && legacyFractionStr != ""
	legacyMemoryStr, hasLegacyMemory := pod.Annotations[constants.GpuMemory]
	hasLegacyMemory = hasLegacyMemory && legacyMemoryStr != ""

	// NvFractions limit cannot coexist with legacy fraction annotations;
	// the prefer semantics only apply to request annotations.
	if req.Limit != nil && (hasLegacyFraction || hasLegacyMemory) {
		return fmt.Errorf("cannot combine %s limit annotation with %s or %s annotation",
			constants.NvFractionsAnnotationPrefix, constants.GpuFraction, constants.GpuMemory)
	}

	// gpu-fraction and gpu-memory request are mutually exclusive.
	if hasLegacyFraction && hasLegacyMemory {
		return fmt.Errorf("cannot request both gpu-fraction and GPU memory request")
	}

	// When both gpu-memory and NvFractions request are present, they must agree on the memory value.
	if hasNvFractionalAnnotations && hasLegacyMemory {
		if err := validateGpuMemoryNvFractionsConsistency(pod); err != nil {
			return err
		}
	}

	return nil
}

func validateGpuMemoryNvFractionsConsistency(pod *v1.Pod) error {
	legacyMemoryStr := pod.Annotations[constants.GpuMemory]
	legacyMemoryMiB, err := strconv.ParseUint(legacyMemoryStr, 10, 64)
	if err != nil || legacyMemoryMiB == 0 {
		return nil
	}
	req, err := ParsePodGPUFractionRequest(pod)
	if err != nil {
		return err
	}
	if req == nil || req.FractionType != FractionTypeNvFractions {
		return fmt.Errorf(
			"cannot validate gpu memory consistency because the pod doesn't have an NvFractions request")
	}
	if req.Memory.Cmp(resource.MustParse(fmt.Sprintf("%dMi", legacyMemoryMiB))) != 0 {
		return fmt.Errorf(
			"NvFractions memory request (%s) does not match %s annotation value (%d MiB)",
			req.Memory.String(), constants.GpuMemory, legacyMemoryMiB,
		)
	}
	return nil
}

// validateGpuMemoryPortionLimitAnnotation validates the kai.scheduler
// gpu-memory.portion.limit annotation: it may only be used together with
// gpu-fraction, on the same container, as a fraction strictly greater than
// gpu-fraction and strictly smaller than 1.0, with at most 5 decimal digits.
func validateGpuMemoryPortionLimitAnnotation(pod *v1.Pod) error {
	containerName, rawValue, found := ExtractGpuMemoryPortionLimitAnnotation(pod)
	if !found {
		return nil
	}
	annotationKey := CalcGpuMemoryPortionLimitAnnotationForContainer(containerName)

	gpuFractionStr, hasGpuFraction := pod.Annotations[constants.GpuFraction]
	if !hasGpuFraction || gpuFractionStr == "" {
		return fmt.Errorf("%s annotation can only be used together with the %s annotation",
			annotationKey, constants.GpuFraction)
	}

	if err := validateGpuMemoryPortionLimitContainerName(pod, containerName, annotationKey); err != nil {
		return err
	}

	gpuFraction, err := strconv.ParseFloat(gpuFractionStr, 64)
	if err != nil {
		return fmt.Errorf("gpu-fraction annotation value must be a positive number smaller than 1.0")
	}

	if err := validatePortionLimitDecimalPrecision(rawValue, annotationKey); err != nil {
		return err
	}

	portionLimit, err := strconv.ParseFloat(rawValue, 64)
	if err != nil || portionLimit <= 0 || portionLimit >= 1 {
		return fmt.Errorf("%s annotation value must be a positive number smaller than 1.0", annotationKey)
	}

	if portionLimit <= gpuFraction {
		return fmt.Errorf("%s annotation value (%s) must be greater than %s annotation value (%s)",
			annotationKey, rawValue, constants.GpuFraction, gpuFractionStr)
	}

	return nil
}

func validateGpuMemoryPortionLimitContainerName(pod *v1.Pod, containerName, annotationKey string) error {
	if legacyContainerName, hasGpuFractionContainerName := pod.Annotations[constants.GpuFractionContainerName]; hasGpuFractionContainerName {
		if legacyContainerName != containerName {
			return fmt.Errorf("%s annotation value %s does not match container name %s in %s annotation",
				constants.GpuFractionContainerName, legacyContainerName, containerName, annotationKey)
		}
		return nil
	}

	if len(pod.Spec.Containers) == 0 || pod.Spec.Containers[0].Name != containerName {
		return fmt.Errorf("%s annotation container name %s does not match the gpu-fraction target container",
			annotationKey, containerName)
	}
	return nil
}

func validatePortionLimitDecimalPrecision(rawValue, annotationKey string) error {
	_, fraction, hasDecimalPoint := strings.Cut(rawValue, ".")
	if hasDecimalPoint && len(fraction) > 5 {
		return fmt.Errorf("%s annotation value must have at most 5 digits after the decimal point", annotationKey)
	}
	return nil
}

func validateNvFractionsAnnotations(hasNvFractionsAnnotation bool, pod *v1.Pod) error {
	if !hasNvFractionsAnnotation {
		return nil
	}

	fractionsData, err := ExtractNvFractionsData(pod)
	if err != nil {
		return err
	}

	for containerName, containerData := range fractionsData {
		if err := validateContainerExists(pod, containerName); err != nil {
			return err
		}

		if containerData.Request != nil && containerData.Limit != nil {
			if containerData.Request.Cmp(*containerData.Limit) > 0 {
				return fmt.Errorf("invalid fraction request for container %s: request is greater than limit", containerName)
			}
		}
	}

	if len(fractionsData) > 1 {
		return fmt.Errorf("currently, kai doesn't support multiple containers with fractional GPU requests")
	}

	if err := validateGpuFractionContainerNameConsistency(pod, fractionsData); err != nil {
		return err
	}

	return nil
}

func validateGpuFractionContainerNameConsistency(pod *v1.Pod, fractionsData map[string]NvFractionsContainerRequest) error {
	if legacyAnnotationContainerName, hasGpuFractionContainerName := pod.Annotations[constants.GpuFractionContainerName]; hasGpuFractionContainerName {
		for nvFractionContainerName := range fractionsData {
			if legacyAnnotationContainerName != nvFractionContainerName {
				return fmt.Errorf("gpu-fraction-container-name annotation value %s does not match container name %s",
					legacyAnnotationContainerName, nvFractionContainerName,
				)
			}
			break // Currently, we support only one container with NvFractions annotations. In one of the previous steps, we validated that.
		}
	}
	return nil
}

func validateContainerExists(pod *v1.Pod, containerName string) error {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return nil
		}
	}
	return fmt.Errorf("container %s not found in pod spec, but a fractional annotation referencing it was found", containerName)
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
