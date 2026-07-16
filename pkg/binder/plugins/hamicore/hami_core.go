// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package hamicore

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

type Plugin struct {
	kubeClient client.Client
}

func New(kubeClient client.Client) *Plugin {
	return &Plugin{kubeClient: kubeClient}
}

func (p *Plugin) Name() string {
	return "hamicore"
}

func (p *Plugin) PreBind(
	ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest, _ *state.BindingState,
) error {
	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	cudaDeviceMemoryLimit, err := calculateCudaDeviceMemoryLimit(node, bindRequest)
	if err != nil {
		return nil
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	return common.SetCudaDeviceMemoryLimit(ctx, p.kubeClient, pod, containerRef, cudaDeviceMemoryLimit)
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

	return fmt.Sprintf("%dm", allocatedMemoryMib), nil
}

func (p *Plugin) PostBind(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) {
}

func (p *Plugin) Rollback(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) error {
	return nil
}
