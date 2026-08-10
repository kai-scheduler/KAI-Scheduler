// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

// NvFractions is the admission plugin for the NvFractions GPU-sharing mode. It
// validates fractional GPU requests (legacy gpu-fraction/gpu-memory,
// gpu-fraction-container-name, and NvFractions annotations) and normalizes
// legacy memory annotations to the NvFractions form, without using the shared
// GPU configmap.
type NvFractions struct{}

func New() *NvFractions {
	return &NvFractions{}
}

func (p *NvFractions) Name() string {
	return "nvfractions"
}

func (p *NvFractions) Validate(pod *v1.Pod) error {
	return resources.ValidateGPUFractionRequest(pod)
}

func (p *NvFractions) Mutate(pod *v1.Pod) error {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}

	if !resources.RequestsGPUFraction(pod) {
		return nil
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	return adjustFractionalMemoryAnnotations(pod, containerRef.Container.Name)
}

// adjustFractionalMemoryAnnotations converts the legacy gpu-memory annotation to
// the NvFractions request form. Kept local to avoid depending on the gpusharing
// plugin's configmap-coupled code.
func adjustFractionalMemoryAnnotations(pod *v1.Pod, containerName string) error {
	gpuMemoryRequestMiB, found := pod.Annotations[constants.GpuMemory]
	if !found {
		return nil
	}

	parsed, err := strconv.ParseUint(gpuMemoryRequestMiB, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse gpu memory annotation value: %w", err)
	}

	memoryQuantity := resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(parsed)
	pod.Annotations[resources.CalcGpuFractionAnnotationForContainer(containerName)] = memoryQuantity.String()
	return nil
}
