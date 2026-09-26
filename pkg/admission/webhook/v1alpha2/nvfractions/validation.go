// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nvfractions

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

func (p *NvFractions) Validate(ctx context.Context, oldPod, pod *v1.Pod) error {
	if err := p.validateDeviceAnnotations(ctx, oldPod, pod); err != nil {
		return err
	}
	return resources.ValidateGPUFractionRequest(pod)
}

func (p *NvFractions) validateDeviceAnnotations(ctx context.Context, oldPod, pod *v1.Pod) error {
	for key := range pod.Annotations {
		if !isDeviceAnnotation(key) {
			continue
		}
		if err := validateDeviceAnnotationTargets(pod); err != nil {
			return err
		}
		if oldPod == nil || annotationAddedOrChanged(key, oldPod, pod) {
			return p.authorizeDeviceAnnotationChange(ctx)
		}
	}
	return nil
}

func (p *NvFractions) authorizeDeviceAnnotationChange(ctx context.Context) error {
	if p.binderServiceAccountUsername == "" {
		return fmt.Errorf("binder service account username is not configured, cannot validate nvfractions device annotations change")
	}

	request, err := admission.RequestFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to extract admission request: %w", err)
	}
	if request.UserInfo.Username != p.binderServiceAccountUsername {
		return fmt.Errorf("%s annotations may only be modified by %s",
			constants.NvFractionsVisibleDevicesSuffix, p.binderServiceAccountUsername)
	}
	return nil
}

func annotationAddedOrChanged(key string, oldPod, pod *v1.Pod) bool {
	oldValue, found := oldPod.Annotations[key]
	return !found || oldValue != pod.Annotations[key]
}

func isDeviceAnnotation(key string) bool {
	return strings.HasPrefix(key, constants.NvFractionsAnnotationPrefix) &&
		strings.HasSuffix(key, constants.NvFractionsVisibleDevicesSuffix)
}

func validateDeviceAnnotationTargets(pod *v1.Pod) error {
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
