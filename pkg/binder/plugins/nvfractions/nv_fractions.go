// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"

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

const bytesPerMiB = 1024 * 1024

// Plugin implements GPU fraction binding for the NvFractions mode. Unlike the
// gpusharing/hamicore plugins it does not use the shared GPU configmap: the
// allocated memory request and the visible devices are passed as pod
// annotations applied after binding.
type Plugin struct {
	gpuDevicePluginUsesCdi bool
	// reservedGpuMemoryMiB is held back from each GPU before portions are
	// resolved to bytes, leaving the MPS server room for its own device context.
	reservedGpuMemoryMiB uint64
}

func New(gpuDevicePluginUsesCdi bool, reservedGpuMemoryMiB uint64) *Plugin {
	return &Plugin{
		gpuDevicePluginUsesCdi: gpuDevicePluginUsesCdi,
		reservedGpuMemoryMiB:   reservedGpuMemoryMiB,
	}
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

	containerRef, err := resources.GetFractionContainerRef(pod)
	if err != nil {
		return fmt.Errorf("failed to get fraction container ref: %w", err)
	}

	if bindingState.BindingPodAnnotations == nil {
		bindingState.BindingPodAnnotations = map[string]string{}
	}

	if err := p.setNvFractionsMemoryAnnotation(pod, node, bindRequest, containerRef.Container.Name, bindingState); err != nil {
		return fmt.Errorf("failed to set NvFractions memory annotation: %w", err)
	}

	if err := p.setGpuMemoryPortionLimitAnnotation(pod, node, containerRef.Container.Name, bindingState); err != nil {
		return fmt.Errorf("failed to set NvFractions memory limit annotation: %w", err)
	}

	p.setGpuComputePortionAnnotation(pod, node, bindRequest, containerRef.Container.Name, bindingState)

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
func (p *Plugin) setNvFractionsMemoryAnnotation(pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
	containerName string, bindingState *state.BindingState) error {
	annotationKey := resources.CalcGpuFractionAnnotationForContainer(containerName)
	if _, found := pod.Annotations[annotationKey]; found {
		return nil
	}

	if node == nil || bindRequest == nil || bindRequest.Spec.ReceivedGPU == nil {
		return fmt.Errorf("missing data for NvFractions annotation calculation")
	}

	usableGPUMemoryMiB, err := p.usableGPUMemoryMiB(node)
	if err != nil {
		return err
	}

	gpuPortion, err := strconv.ParseFloat(bindRequest.Spec.ReceivedGPU.Portion, 64)
	if err != nil || gpuPortion <= 0 {
		return fmt.Errorf("invalid received gpu portion %q", bindRequest.Spec.ReceivedGPU.Portion)
	}

	gpuMemory := uint64(usableGPUMemoryMiB * gpuPortion)
	if gpuMemory == 0 {
		return fmt.Errorf("calculated gpu memory request is zero")
	}

	bindingState.BindingPodAnnotations[annotationKey] = resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemory).String()
	return nil
}

// usableGPUMemoryMiB is the node's per-GPU memory minus the reserve. Errors when
// the label is missing or unusable, or when the reserve leaves nothing to split.
func (p *Plugin) usableGPUMemoryMiB(node *v1.Node) (float64, error) {
	gpuMemoryStr, found := node.Labels[constants.NvidiaGpuMemory]
	if !found {
		return 0, fmt.Errorf("node does not include %s label", constants.NvidiaGpuMemory)
	}

	totalGPUMemoryMiB, err := strconv.ParseFloat(gpuMemoryStr, 64)
	if err != nil || totalGPUMemoryMiB <= 0 {
		return 0, fmt.Errorf("invalid %s label value %q", constants.NvidiaGpuMemory, gpuMemoryStr)
	}

	usable := totalGPUMemoryMiB - float64(p.reservedGpuMemoryMiB)
	if usable <= 0 {
		return 0, fmt.Errorf("reserved gpu memory %dMiB leaves no allocatable memory on a %sMiB GPU",
			p.reservedGpuMemoryMiB, gpuMemoryStr)
	}
	return usable, nil
}

// setGpuMemoryPortionLimitAnnotation translates the kai.scheduler
// gpu-memory.portion.limit annotation into the NvFractions limit form,
// without removing the source annotation.
func (p *Plugin) setGpuMemoryPortionLimitAnnotation(pod *v1.Pod, node *v1.Node, containerName string, bindingState *state.BindingState) error {
	_, rawPortionLimit, found := resources.ExtractGpuMemoryPortionLimitAnnotation(pod)
	if !found {
		return nil
	}

	annotationKey := resources.CalcGpuFractionLimitAnnotationForContainer(containerName)
	if _, found := pod.Annotations[annotationKey]; found {
		return nil
	}

	if node == nil {
		return fmt.Errorf("missing node data for gpu-memory.portion.limit annotation calculation")
	}

	usableGPUMemoryMiB, err := p.usableGPUMemoryMiB(node)
	if err != nil {
		return err
	}

	portionLimit, err := strconv.ParseFloat(rawPortionLimit, 64)
	if err != nil || portionLimit <= 0 {
		return fmt.Errorf("invalid gpu-memory.portion.limit annotation value %q", rawPortionLimit)
	}

	gpuMemoryLimit := uint64(usableGPUMemoryMiB * portionLimit)
	if gpuMemoryLimit == 0 {
		return fmt.Errorf("calculated gpu memory limit is zero")
	}

	bindingState.BindingPodAnnotations[annotationKey] = resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemoryLimit).String()
	return nil
}

// setGpuComputePortionAnnotation records the GPU compute share the workload is
// entitled to, which kai-gpu-fractioning enforces as an MPS active-thread
// percentage. Without it a container holding a fraction of a GPU's memory can
// still occupy every SM on the card.
//
// The received portion is authoritative; a memory request resolved against the
// node's usable per-GPU memory is the fallback for binds that carry no portion.
// Neither being available means no compute cap rather than a failed bind.
//
// An existing value is overwritten rather than preserved, unlike the memory
// request. This is a limit imposed on the workload, not a request made by it, so
// preserving a self-declared one would leave admission as the only thing between
// a pod manifest and its own compute cap. Recomputing is idempotent on rebind.
func (p *Plugin) setGpuComputePortionAnnotation(pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
	containerName string, bindingState *state.BindingState) {
	portion, found := p.resolveComputePortion(pod, node, bindRequest, containerName, bindingState)
	if !found {
		return
	}

	annotationKey := resources.CalcGpuComputePortionAnnotationForContainer(containerName)
	bindingState.BindingPodAnnotations[annotationKey] = strconv.FormatFloat(portion, 'f', -1, 64)
}

func (p *Plugin) resolveComputePortion(pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest,
	containerName string, bindingState *state.BindingState) (float64, bool) {
	if bindRequest != nil && bindRequest.Spec.ReceivedGPU != nil {
		if portion, err := strconv.ParseFloat(bindRequest.Spec.ReceivedGPU.Portion, 64); err == nil && portion > 0 {
			return math.Min(portion, 1), true
		}
	}

	if node == nil {
		return 0, false
	}
	usableGPUMemoryMiB, err := p.usableGPUMemoryMiB(node)
	if err != nil {
		return 0, false
	}

	memoryMiB, found := resolvedMemoryRequestMiB(pod, containerName, bindingState)
	if !found {
		return 0, false
	}

	portion := float64(memoryMiB) / usableGPUMemoryMiB
	if portion <= 0 {
		return 0, false
	}
	return math.Min(portion, 1), true
}

// resolvedMemoryRequestMiB reads the container's NvFractions memory request from
// the annotations about to be applied, falling back to the pod's existing ones.
func resolvedMemoryRequestMiB(pod *v1.Pod, containerName string, bindingState *state.BindingState) (uint64, bool) {
	annotationKey := resources.CalcGpuFractionAnnotationForContainer(containerName)

	raw, found := bindingState.BindingPodAnnotations[annotationKey]
	if !found {
		raw, found = pod.Annotations[annotationKey]
	}
	if !found {
		return 0, false
	}

	quantity, err := apiresource.ParseQuantity(raw)
	if err != nil || quantity.Sign() <= 0 {
		return 0, false
	}
	return uint64(quantity.Value()) / bytesPerMiB, true
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
