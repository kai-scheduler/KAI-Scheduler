// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"fmt"

	"strconv"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
)

const (
	CdiDeviceNameBase = "k8s.device-plugin.nvidia.com/gpu=%s"
)

type GPUSharing struct {
	kubeClient        client.Client
	gpuSharingEnabled bool
}

func New(kubeClient client.Client, gpuSharingEnabled bool) *GPUSharing {
	return &GPUSharing{
		kubeClient:        kubeClient,
		gpuSharingEnabled: gpuSharingEnabled,
	}
}

func (p *GPUSharing) Name() string {
	return "gpusharing"
}

func (p *GPUSharing) Validate(pod *v1.Pod) error {
	if !p.gpuSharingEnabled && resources.RequestsGPUFraction(pod) {
		return fmt.Errorf(
			"attempting to create a pod %s/%s with gpu sharing request, while GPU sharing is disabled",
			pod.Namespace, pod.Name,
		)
	}
	return resources.ValidateGPUFractionRequest(pod)
}

func (p *GPUSharing) Mutate(pod *v1.Pod) error {
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

	err = adjustFractionalMemoryAnnotations(pod, containerRef)
	if err != nil {
		return err
	}

	capabilitiesConfigMapName := gpusharingconfigmap.SetGpuCapabilitiesConfigMapName(pod, containerRef)
	directEnvVarsMapName, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	if err != nil {
		return err
	}

	common.AddGPUSharingEnvVars(containerRef.Container, capabilitiesConfigMapName)
	common.SetConfigMapVolume(pod, capabilitiesConfigMapName)
	common.AddDirectEnvVarsConfigMapSource(containerRef.Container, directEnvVarsMapName)

	return nil
}

// adjustFractionalMemoryAnnotations adjusts the old fractional memory annotations to NvFractions format
func adjustFractionalMemoryAnnotations(pod *v1.Pod, containerRef *gpusharingconfigmap.PodContainerRef) error {
	gpuMemoryRequestMiB, foundGPUMemory := pod.Annotations[constants.GpuMemory]
	if foundGPUMemory {
		gpuMemoryRequestMiB, err := strconv.ParseUint(gpuMemoryRequestMiB, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse gpu memory annotation value: %w", err)
		}
		memoryQuantity := resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemoryRequestMiB)
		pod.Annotations[resources.CalcGpuFractionAnnotationForContainer(containerRef.Container.Name)] = memoryQuantity.String()
	}
	return nil
}
