// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package runtimeenforcement

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
)

type RuntimeEnforcement struct {
	gpuPodRuntimeClassName string
}

func New(gpuPodRuntimeClassName string) *RuntimeEnforcement {
	return &RuntimeEnforcement{
		gpuPodRuntimeClassName: gpuPodRuntimeClassName,
	}
}

func (p *RuntimeEnforcement) Name() string {
	return "runtimeenforcement"
}

func (p *RuntimeEnforcement) Validate(pod *v1.Pod) error {
	return nil
}

func (p *RuntimeEnforcement) Mutate(pod *v1.Pod) error {
	// Skip runtimeClassName injection entirely when configured with empty string
	if p.gpuPodRuntimeClassName == "" {
		return nil
	}

	// in order to no collide with custom reservation pods runtimeClass
	if resourcereservation.IsGPUReservationPod(pod) {
		return nil
	}

	if !resources.RequestsGPU(pod) {
		return nil
	}

	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName == "" {
		setRuntimeClass(pod, p.gpuPodRuntimeClassName)
		return nil
	}

	return nil
}

func setRuntimeClass(pod *v1.Pod, runtimeClassName string) {
	pod.Spec.RuntimeClassName = ptr.To(runtimeClassName)
}
