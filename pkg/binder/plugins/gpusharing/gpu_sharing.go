// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const (
	CdiDeviceNameBase = "k8s.device-plugin.nvidia.com/gpu=%s"
)

type GPUSharing struct {
	kubeClient             client.Client
	gpuDevicePluginUsesCdi bool
	hamiCoreEnabled        bool
}

func New(kubeClient client.Client, gpuDevicePluginUsesCdi bool, hamiCoreEnabled bool) *GPUSharing {
	return &GPUSharing{
		kubeClient:             kubeClient,
		gpuDevicePluginUsesCdi: gpuDevicePluginUsesCdi,
		hamiCoreEnabled:        hamiCoreEnabled,
	}
}

func (p *GPUSharing) Name() string {
	return "gpusharing"
}

func (p *GPUSharing) PreBind(
	ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest, state *state.BindingState,
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

	err = common.SetGPUPortion(ctx, p.kubeClient, pod, containerRef, bindRequest.Spec.ReceivedGPU.Portion)
	if err != nil {
		return err
	}

	if !p.hamiCoreEnabled {
		return nil
	}

	cudaDeviceMemoryLimit, err := calculateCudaDeviceMemoryLimit(node, bindRequest)
	if err != nil {
		return nil
	}

	err = common.SetCudaDeviceMemoryLimit(ctx, p.kubeClient, pod, containerRef, cudaDeviceMemoryLimit)
	if err != nil {
		return err
	}

	return nil
}

func calculateCudaDeviceMemoryLimit(node *v1.Node, bindRequest *v1alpha2.BindRequest) (string, error) {
	if node == nil || bindRequest == nil || bindRequest.Spec.ReceivedGPU == nil {
		return "", fmt.Errorf("missing data for CUDA_DEVICE_MEMORY_LIMIT calculation")
	}

	memoryLabel, found := node.Labels[constants.NvidiaGpuMemory]
	if !found {
		return "", fmt.Errorf("node does not include %s label", constants.NvidiaGpuMemory)
	}

	totalGPUMemoryMib, err := strconv.ParseInt(memoryLabel, 10, 64)
	if err != nil || totalGPUMemoryMib <= 0 {
		return "", fmt.Errorf("invalid %s label value %q", constants.NvidiaGpuMemory, memoryLabel)
	}

	gpuPortion, err := strconv.ParseFloat(bindRequest.Spec.ReceivedGPU.Portion, 64)
	if err != nil || gpuPortion <= 0 {
		return "", fmt.Errorf("invalid received gpu portion %q", bindRequest.Spec.ReceivedGPU.Portion)
	}

	allocatedMemoryMib := int64(float64(totalGPUMemoryMib) * gpuPortion)
	if allocatedMemoryMib <= 0 {
		return "", fmt.Errorf("calculated allocated gpu memory is zero")
	}

	return strconv.FormatInt(allocatedMemoryMib, 10), nil
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

func (p *GPUSharing) Rollback(
	ctx context.Context, pod *v1.Pod, _ *v1.Node, bindRequest *v1alpha2.BindRequest, _ *state.BindingState,
) error {
	logger := log.FromContext(ctx)

	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	var errs []error

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		logger.V(1).Info("Rollback: could not get fraction container ref, nothing to rollback",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
		return nil
	}

	var configMapNames []string
	capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		logger.V(1).Info("could not extract capabilities configmap name",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
	} else if capabilitiesConfigMapName != "" {
		configMapNames = append(configMapNames, capabilitiesConfigMapName)
	}

	directEnvVarsMapName, err := gpusharingconfigmap.ExtractDirectEnvVarsConfigMapName(pod, containerRef)
	if err != nil {
		logger.V(1).Info("could not extract direct env vars configmap name",
			"namespace", pod.Namespace, "name", pod.Name, "error", err)
	} else if directEnvVarsMapName != "" {
		configMapNames = append(configMapNames, directEnvVarsMapName)
	}

	for _, configMapName := range configMapNames {
		if err = p.deleteConfigMap(ctx, pod.Namespace, configMapName); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete configmap %s/%s during rollback: %w",
				pod.Namespace, configMapName, err))
		}
		logger.V(1).Info("deleted configmap", "namespace", pod.Namespace, "name", pod.Name, "configmap", configMapName)
	}

	return errors.Join(errs...)
}

func (p *GPUSharing) deleteConfigMap(ctx context.Context, namespace, name string) error {
	cm := &v1.ConfigMap{}
	cm.Name = name
	cm.Namespace = namespace
	return client.IgnoreNotFound(p.kubeClient.Delete(ctx, cm))
}
