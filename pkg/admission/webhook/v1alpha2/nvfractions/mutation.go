// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

func (p *NvFractions) Mutate(pod *v1.Pod) error {
	if len(pod.Spec.Containers) == 0 || !resources.RequestsGPUFraction(pod) {
		return nil
	}

	containerRef, err := resources.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	err = translateLegacyMemory(pod, containerRef.Container.Name)
	if err != nil {
		return err
	}
	err = translateLegacyMemoryLimit(pod, containerRef.Container.Name)
	if err != nil {
		return err
	}
	err = translateLegacyGpuFractionLimit(pod, containerRef.Container.Name)
	if err != nil {
		return err
	}
	return nil
}

func translateLegacyMemory(pod *v1.Pod, containerName string) error {
	value, found, err := legacyMemoryRequest(pod.Annotations)
	if err != nil || !found {
		return err
	}
	target := resources.CalcGpuFractionAnnotationForContainer(containerName)
	if _, exists := pod.Annotations[target]; !exists {
		pod.Annotations[target] = value
	} else if pod.Annotations[target] != value {
		return fmt.Errorf("gpu-memory annotation conflicts with %s annotation", target)
	}
	return nil
}

func legacyMemoryRequest(annotations map[string]string) (string, bool, error) {
	value, found := annotations[constants.GpuMemory]
	if !found {
		return "", false, nil
	}

	memoryMiB, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return "", true, fmt.Errorf("failed to parse gpu memory annotation value: %w", err)
	}

	memory := resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(memoryMiB)
	return memory.String(), true, nil
}

func translateLegacyMemoryLimit(pod *v1.Pod, containerName string) error {
	value, found := pod.Annotations[constants.GpuMemoryLimit]
	if !found {
		return nil
	}

	target := resources.CalcGpuFractionLimitAnnotationForContainer(containerName)
	if _, exists := pod.Annotations[target]; !exists {
		pod.Annotations[target] = value
	} else if pod.Annotations[target] != value {
		return fmt.Errorf("gpu-memory.limit annotation conflicts with %s annotation", target)
	}
	return nil
}

func translateLegacyGpuFractionLimit(pod *v1.Pod, containerName string) error {
	value, found := pod.Annotations[constants.GpuFractionLimit]
	if !found {
		return nil
	}

	target := resources.CalcGpuMemoryPortionLimitAnnotationForContainer(containerName)
	if _, exists := pod.Annotations[target]; !exists {
		pod.Annotations[target] = value
	} else if pod.Annotations[target] != value {
		return fmt.Errorf("gpu-fraction.limit annotation conflicts with %s annotation", target)
	}
	return nil
}
