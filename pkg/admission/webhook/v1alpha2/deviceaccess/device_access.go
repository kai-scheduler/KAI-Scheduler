// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deviceaccess

import (
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common/gpusharingconfigmap"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

var visibleDevicesWhitelist = []string{"void", "none"}

type DeviceAccess struct{}

func New() *DeviceAccess {
	return &DeviceAccess{}
}

func (da *DeviceAccess) Name() string {
	return "deviceaccess"
}

func (da *DeviceAccess) Validate(pod *v1.Pod) error {
	requestsFraction := resources.RequestsGPUFraction(pod)

	var containerRef *gpusharingconfigmap.PodContainerRef
	if requestsFraction {
		var err error
		containerRef, err = common.GetFractionContainerRef(pod)
		if err != nil {
			return fmt.Errorf("failed to get fraction container ref: %w", err)
		}
	}

	for containerIndex := range pod.Spec.InitContainers {
		if requestsFraction && containerRef.Type == gpusharingconfigmap.InitContainer && containerIndex == containerRef.Index {
			continue
		}

		err := validateSingleContainer(&pod.Spec.InitContainers[containerIndex])
		if err != nil {
			return err
		}
	}

	for containerIndex := range pod.Spec.Containers {
		if requestsFraction && containerRef.Type == gpusharingconfigmap.RegularContainer && containerIndex == containerRef.Index {
			continue
		}

		err := validateSingleContainer(&pod.Spec.Containers[containerIndex])
		if err != nil {
			return err
		}
	}

	return nil
}

func (da *DeviceAccess) Mutate(pod *v1.Pod) error {
	requestsFraction := resources.RequestsGPUFraction(pod)

	var containerRef *gpusharingconfigmap.PodContainerRef
	if requestsFraction {
		var err error
		containerRef, err = common.GetFractionContainerRef(pod)
		if err != nil {
			return fmt.Errorf("failed to get fraction container ref: %w", err)
		}
	}

	for containerIndex := range pod.Spec.InitContainers {
		if requestsFraction && containerRef.Type == gpusharingconfigmap.InitContainer && containerIndex == containerRef.Index {
			continue
		}
		blockGPUAccessIfNotRequested(&pod.Spec.InitContainers[containerIndex])
	}

	for containerIndex := range pod.Spec.Containers {
		if requestsFraction && containerRef.Type == gpusharingconfigmap.RegularContainer && containerIndex == containerRef.Index {
			continue
		}
		blockGPUAccessIfNotRequested(&pod.Spec.Containers[containerIndex])
	}

	return nil
}

// blockGPUAccessIfNotRequested sets NVIDIA_VISIBLE_DEVICES=void on containers that do not
// request a GPU, preventing them from accessing GPUs on the node.
func blockGPUAccessIfNotRequested(container *v1.Container) {
	if containerRequestsGPU(container) {
		return
	}
	setVisibleDevicesEnvVar(container, "void")
}

func containerRequestsGPU(container *v1.Container) bool {
	if qty, found := container.Resources.Requests[v1.ResourceName(constants.NvidiaGpuResource)]; found && !qty.IsZero() {
		return true
	}
	for name, qty := range container.Resources.Requests {
		if resources.IsMigResource(name.String()) && !qty.IsZero() {
			return true
		}
	}
	return false
}

func setVisibleDevicesEnvVar(container *v1.Container, value string) {
	for i, env := range container.Env {
		if env.Name == constants.NvidiaVisibleDevices {
			container.Env[i].Value = value
			return
		}
	}
	container.Env = append(container.Env, v1.EnvVar{Name: constants.NvidiaVisibleDevices, Value: value})
}

func validateSingleContainer(container *v1.Container) error {
	for _, envVar := range container.Env {
		if envVar.Name == constants.NvidiaVisibleDevices {
			if err := whitelistVisibleDevicesEnvVar(container, envVar); err != nil {
				return err
			}
		}
	}
	return nil
}

func whitelistVisibleDevicesEnvVar(container *v1.Container, envVar v1.EnvVar) error {
	if envVar.Value != "" {
		if !slices.Contains(visibleDevicesWhitelist, envVar.Value) {
			return fmt.Errorf(
				"container %s has an environment variable NVIDIA_VISIBLE_DEVICES"+
					" defined with a value of %s. This is forbidden due to conflicts with Nvidia's device plugin."+
					" The only values that are allowed are 'void' or 'none'",
				container.Name, envVar.Value)
		}
	} else if envVar.ValueFrom != nil {
		return fmt.Errorf(
			"container %s has an environment variable NVIDIA_VISIBLE_DEVICES defined "+
				"with a valueFrom reference. "+
				"This is forbidden due to possible conflicts with Nvidia's device plugin",
			container.Name)
	}

	return nil
}
