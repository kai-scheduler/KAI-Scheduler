// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

// cdiDeviceNameFormat qualifies a reserved GPU id as a CDI device of the
// device-plugin kind (matching gpusharing.CdiDeviceNameBase). That CDI spec
// carries the apply-cuda-memory-limits hook that enforces the memory limit; the
// runtime's default kind (management.nvidia.com/gpu) does not, so a bare UUID
// would neither resolve nor enforce for a fractional pod.
const cdiDeviceNameFormat = "k8s.device-plugin.nvidia.com/gpu=%s"

// Plugin implements GPU fraction binding for the NvFractions mode. Unlike the
// gpusharing/hamicore plugins it does not use the shared GPU configmap: the
// allocated memory request and the visible devices are passed as pod
// annotations applied after binding.
type Plugin struct {
	gpuDevicePluginUsesCdi bool
}

func New(gpuDevicePluginUsesCdi bool) *Plugin {
	return &Plugin{gpuDevicePluginUsesCdi: gpuDevicePluginUsesCdi}
}

func (p *Plugin) Name() string {
	return "nvfractions"
}

func (p *Plugin) PreBind(
	_ context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest, bindingState *state.BindingState,
) error {
	if !common.IsSharedGPUAllocation(bindRequest) {
		return nil
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	if bindingState.BindingPodAnnotations == nil {
		bindingState.BindingPodAnnotations = map[string]string{}
	}

	if err := setNvFractionsMemoryAnnotation(pod, node, bindRequest, containerRef.Container.Name, bindingState); err != nil {
		return fmt.Errorf("failed to set NvFractions memory annotation: %w", err)
	}

	visibleDevices := bindingState.ReservedGPUIds
	if p.gpuDevicePluginUsesCdi {
		visibleDevices = make([]string, len(bindingState.ReservedGPUIds))
		for i, gpuID := range bindingState.ReservedGPUIds {
			visibleDevices[i] = fmt.Sprintf(cdiDeviceNameFormat, gpuID)
		}
	}
	visibleDevicesAnnotation := resources.CalcGpuVisibleDevicesAnnotationForContainer(containerRef.Container.Name)
	bindingState.BindingPodAnnotations[visibleDevicesAnnotation] = strings.Join(visibleDevices, ",")

	return nil
}

// setNvFractionsMemoryAnnotation computes the allocated GPU memory from the node
// total and the received portion and records it as the NvFractions request
// annotation. The arithmetic is intentionally kept local to avoid depending on
// the gpusharing/hamicore plugins.
func setNvFractionsMemoryAnnotation(pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
	containerName string, bindingState *state.BindingState) error {
	annotationKey := resources.CalcGpuFractionAnnotationForContainer(containerName)
	if _, found := pod.Annotations[annotationKey]; found {
		return nil
	}

	if node == nil || bindRequest == nil || bindRequest.Spec.ReceivedGPU == nil {
		return fmt.Errorf("missing data for NvFractions annotation calculation")
	}

	gpuMemoryStr, found := node.Labels[constants.NvidiaGpuMemory]
	if !found {
		return fmt.Errorf("node does not include %s label", constants.NvidiaGpuMemory)
	}

	totalGPUMemoryMiB, err := strconv.ParseFloat(gpuMemoryStr, 64)
	if err != nil || totalGPUMemoryMiB <= 0 {
		return fmt.Errorf("invalid %s label value %q", constants.NvidiaGpuMemory, gpuMemoryStr)
	}

	gpuPortion, err := strconv.ParseFloat(bindRequest.Spec.ReceivedGPU.Portion, 64)
	if err != nil || gpuPortion <= 0 {
		return fmt.Errorf("invalid received gpu portion %q", bindRequest.Spec.ReceivedGPU.Portion)
	}

	gpuMemory := uint64(totalGPUMemoryMiB * gpuPortion)
	if gpuMemory == 0 {
		return fmt.Errorf("calculated gpu memory request is zero")
	}

	bindingState.BindingPodAnnotations[annotationKey] = resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemory).String()
	return nil
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
