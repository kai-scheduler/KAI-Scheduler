// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
)

const (
	CdiDeviceNameBase = "k8s.device-plugin.nvidia.com/gpu=%s"
)

type GPUSharing struct {
	kubeClient             client.Client
	gpuDevicePluginUsesCdi bool
}

func New(kubeClient client.Client, gpuDevicePluginUsesCdi bool) *GPUSharing {
	return &GPUSharing{
		kubeClient:             kubeClient,
		gpuDevicePluginUsesCdi: gpuDevicePluginUsesCdi,
	}
}

func (p *GPUSharing) Name() string {
	return "gpusharing"
}

func (p *GPUSharing) PreBind(
	ctx context.Context, pod *v1.Pod, _ *v1.Node, bindRequest *v1alpha2.BindRequest, state *state.BindingState,
) error {
	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	reservedGPUIds := slices.Clone(state.ReservedGPUIds)
	if p.gpuDevicePluginUsesCdi {
		for index, gpuIndex := range reservedGPUIds {
			reservedGPUIds[index] = fmt.Sprintf(CdiDeviceNameBase, gpuIndex)
		}
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	err = p.createCapabilitiesConfigMapIfMissing(ctx, pod, containerRef)
	if err != nil {
		return fmt.Errorf("failed to create capabilities configmap: %w", err)
	}

	err = p.createDirectEnvMapIfMissing(ctx, pod, containerRef)
	if err != nil {
		return fmt.Errorf("failed to create env configmap: %w", err)
	}

	nVisibleDevicesStr := strings.Join(reservedGPUIds, ",")
	err = common.SetNvidiaVisibleDevices(ctx, p.kubeClient, pod, containerRef, nVisibleDevicesStr)
	if err != nil {
		return err
	}

	return common.SetGPUPortion(ctx, p.kubeClient, pod, containerRef, bindRequest.Spec.ReceivedGPU.Portion)
}

func (p *GPUSharing) createCapabilitiesConfigMapIfMissing(ctx context.Context, pod *v1.Pod,
	containerRef *gpusharingconfigmap.PodContainerRef) error {
	capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		return fmt.Errorf("failed to get capabilities configmap name: %w", err)
	}
	err = gpusharingconfigmap.UpsertJobConfigMap(ctx, p.kubeClient, pod, capabilitiesConfigMapName, map[string]string{})
	return err
}

func (p *GPUSharing) createDirectEnvMapIfMissing(ctx context.Context, pod *v1.Pod,
	containerRef *gpusharingconfigmap.PodContainerRef) error {
	directEnvVarsMapName, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	if err != nil {
		return err
	}
	directEnvVars := make(map[string]string)
	return gpusharingconfigmap.UpsertJobConfigMap(ctx, p.kubeClient, pod, directEnvVarsMapName, directEnvVars)
}

func (p *GPUSharing) PostBind(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) {
}
