// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package hamicore

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/api/utilities/resources"
)

type HamiCore struct{}

func New() *HamiCore {
	return &HamiCore{}
}

func (p *HamiCore) Name() string {
	return "hamicore"
}

func (p *HamiCore) Validate(_ *v1.Pod) error {
	return nil
}

// Mutate injects the CUDA_DEVICE_MEMORY_LIMIT env var into the fractional GPU
// container. The env var references the capabilities ConfigMap key that the
// hamicore binder plugin populates at bind time. The ConfigMap itself and its
// annotation are set up by the gpusharing admission plugin, so this plugin must
// run after gpusharing.
func (p *HamiCore) Mutate(pod *v1.Pod) error {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}

	if !resources.RequestsGPUFraction(pod) {
		return nil
	}

	containerRef, err := common.GetFractionContainerRef(pod)
	if err != nil {
		return err
	}

	capabilitiesConfigMapName, err := gpusharingconfigmap.ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		return err
	}

	common.AddEnvVarToContainer(containerRef.Container, v1.EnvVar{
		Name: common.CudaDeviceMemoryLimit,
		ValueFrom: &v1.EnvVarSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				Key: common.CudaDeviceMemoryLimit,
				LocalObjectReference: v1.LocalObjectReference{
					Name: capabilitiesConfigMapName,
				},
				Optional: ptr.To(true),
			},
		},
	})

	return nil
}
