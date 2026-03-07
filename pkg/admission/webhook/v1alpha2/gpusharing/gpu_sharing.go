// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/resources"

	"github.com/kai-scheduler/kai-scheduler/pkg/binder/common"
	gpurequesthandler "github.com/kai-scheduler/kai-scheduler/pkg/binder/plugins/gpusharing/gpu-request"
)

const (
	fractionContainerIndex = 0
	CdiDeviceNameBase      = "k8s.device-plugin.nvidia.com/gpu=%s"
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
	return gpurequesthandler.ValidateGpuRequests(pod)
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
