// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

// NvFractions is the admission plugin for the NvFractions GPU-sharing mode. It
// validates fractional GPU requests (legacy gpu-fraction/gpu-memory,
// gpu-fraction-container-name, and NvFractions annotations) and normalizes
// legacy memory annotations to the NvFractions form, without using the shared
// GPU configmap.
type NvFractions struct {
	binderServiceAccountUsername string
}

func New(binderServiceAccountUsername string) *NvFractions {
	return &NvFractions{
		binderServiceAccountUsername: binderServiceAccountUsername,
	}
}

func (p *NvFractions) Name() string {
	return "nvfractions"
}

func (p *NvFractions) Validate(ctx context.Context, oldPod, pod *v1.Pod) error {
	if err := p.validateDeviceAnnotation(ctx, oldPod, pod); err != nil {
		return err
	}
	return resources.ValidateGPUFractionRequest(stripNvFractionsDeviceAnnotations(pod))
}

func (p *NvFractions) Mutate(pod *v1.Pod) error {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}

	if !resources.RequestsGPUFraction(pod) {
		return nil
	}

	containerRef, err := resources.GetFractionContainerRef(pod)
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

// validateDeviceAnnotation makes sure that only the binder service account can modify the binder-owned
// nvfractions annotations: the device list, which grants GPU access, and the compute portion, which caps
// GPU compute. Both are limits a workload is subject to, so a workload able to set them could exempt itself.
func (p *NvFractions) validateDeviceAnnotation(ctx context.Context, oldPod, pod *v1.Pod) error {
	hasNvFractionsDeviceAnnotationChange := false
	changedAnnotationKey := ""
	for annotationKey := range pod.Annotations {
		if !resources.IsBinderOwnedNvFractionsAnnotation(annotationKey) {
			continue
		}
		if err := validateDeviceAnnotationContainerExists(pod); err != nil {
			return err
		}

		if oldPod != nil {
			hasNvFractionsDeviceAnnotationChange = isAnnotationChanged(annotationKey, oldPod, pod)
		} else {
			hasNvFractionsDeviceAnnotationChange = true
		}

		if hasNvFractionsDeviceAnnotationChange {
			changedAnnotationKey = annotationKey
			break
		}
	}

	if !hasNvFractionsDeviceAnnotationChange && oldPod != nil {
		// Removal is a modification too: dropping the compute portion would lift the
		// workload's own GPU compute cap, and dropping the device list its scoping.
		for annotationKey := range oldPod.Annotations {
			if !resources.IsBinderOwnedNvFractionsAnnotation(annotationKey) {
				continue
			}
			if _, found := pod.Annotations[annotationKey]; !found {
				hasNvFractionsDeviceAnnotationChange = true
				changedAnnotationKey = annotationKey
				break
			}
		}
	}

	if !hasNvFractionsDeviceAnnotationChange {
		return nil
	}

	if len(p.binderServiceAccountUsername) == 0 {
		return fmt.Errorf("binder service account username is not configured, cannot validate nvfractions device annotations change")
	}

	request, err := admission.RequestFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to extract admission request: %w", err)
	}
	if request.UserInfo.Username != p.binderServiceAccountUsername {
		return fmt.Errorf("%s annotations may only be modified by %s",
			binderOwnedSuffix(changedAnnotationKey), p.binderServiceAccountUsername)
	}

	return nil
}

func isAnnotationChanged(annotationKey string, oldPod *v1.Pod, pod *v1.Pod) bool {
	oldAnnotationValue, oldHasNvFractionsDeviceAnnotation := oldPod.Annotations[annotationKey]
	if !oldHasNvFractionsDeviceAnnotation {
		return true
	}
	newAnnotationValue := pod.Annotations[annotationKey]
	if oldAnnotationValue != newAnnotationValue {
		return true
	}
	return false
}

func stripNvFractionsDeviceAnnotations(pod *v1.Pod) *v1.Pod {
	if pod == nil || len(pod.Annotations) == 0 {
		return pod
	}

	podCopy := pod.DeepCopy()
	for key := range podCopy.Annotations {
		if resources.IsBinderOwnedNvFractionsAnnotation(key) {
			delete(podCopy.Annotations, key)
		}
	}
	return podCopy
}

// binderOwnedSuffix names the offending annotation in the rejection message.
func binderOwnedSuffix(annotationKey string) string {
	if strings.HasSuffix(annotationKey, constants.NvFractionsComputePortionSuffix) {
		return constants.NvFractionsComputePortionSuffix
	}
	return constants.NvFractionsVisibleDevicesSuffix
}

func validateDeviceAnnotationContainerExists(pod *v1.Pod) error {
	containerName, _, err := resources.GetNvFractionsContainerName(pod.Annotations)
	if err != nil {
		return err
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return nil
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if container.Name == containerName {
			return nil
		}
	}
	return fmt.Errorf("container %s not found in pod spec, but a fractional annotation referencing it was found", containerName)
}
