// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package runtimeenforcement

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/api/utilities/resources"
)

type RuntimeEnforcement struct {
	gpuFractionRuntimeClassName string
}

func New(gpuFractionRuntimeClassName string) *RuntimeEnforcement {
	return &RuntimeEnforcement{
		gpuFractionRuntimeClassName: gpuFractionRuntimeClassName,
	}
}

func (p *RuntimeEnforcement) Name() string {
	return "runtimeenforcement"
}

func (p *RuntimeEnforcement) Validate(pod *v1.Pod) error {
	return nil
}

func (p *RuntimeEnforcement) Mutate(pod *v1.Pod) error {
	if p.gpuFractionRuntimeClassName == "" {
		return nil
	}

	if !resources.RequestsGPUFraction(pod) {
		return nil
	}

	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName == "" {
		setRuntimeClass(pod, p.gpuFractionRuntimeClassName)
		return nil
	}

	return nil
}

func setRuntimeClass(pod *v1.Pod, runtimeClassName string) {
	pod.Spec.RuntimeClassName = ptr.To(runtimeClassName)
}
