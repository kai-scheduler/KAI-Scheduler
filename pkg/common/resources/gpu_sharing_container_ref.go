// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type ContainerType string

const (
	RegularContainer ContainerType = "RegularContainer"
	InitContainer    ContainerType = "InitContainer"

	defaultFractionContainer = 0
)

type PodContainerRef struct {
	Container *v1.Container
	Index     int
	Type      ContainerType
}

func GetFractionContainerRef(pod *v1.Pod) (*PodContainerRef, error) {
	defaultContainerRef := &PodContainerRef{
		Container: &pod.Spec.Containers[defaultFractionContainer],
		Index:     defaultFractionContainer,
		Type:      RegularContainer,
	}

	name, found := pod.Annotations[constants.GpuFractionContainerName]
	nvFractionsName, hasNvFractionsName, err := GetNvFractionsContainerName(pod.Annotations)
	if err != nil {
		return nil, err
	}
	if found && hasNvFractionsName && name != nvFractionsName {
		return nil, fmt.Errorf("gpu-fraction-container-name annotation value %s does not match container name %s",
			name, nvFractionsName)
	}
	if hasNvFractionsName {
		return getContainerRefByName(pod, nvFractionsName)
	}
	if !found {
		return defaultContainerRef, nil
	}

	return getContainerRefByName(pod, name)
}

func GetNvFractionsContainerName(annotations map[string]string) (string, bool, error) {
	var containerName string
	for annotationKey := range annotations {
		if !strings.HasPrefix(annotationKey, constants.NvFractionsAnnotationPrefix) {
			continue
		}

		currentName, _, err := parseNvFractionsAnnotationKey(annotationKey)
		if err != nil {
			return "", false, err
		}
		if containerName != "" && containerName != currentName {
			return "", false, fmt.Errorf("currently, kai doesn't support multiple containers with fractional GPU requests")
		}
		containerName = currentName
	}

	return containerName, containerName != "", nil
}

func getContainerRefByName(pod *v1.Pod, name string) (*PodContainerRef, error) {
	for index, container := range pod.Spec.InitContainers {
		if container.Name != name {
			continue
		}

		return &PodContainerRef{
			Container: &pod.Spec.InitContainers[index],
			Index:     index,
			Type:      InitContainer,
		}, nil
	}

	for index, container := range pod.Spec.Containers {
		if container.Name != name {
			continue
		}

		return &PodContainerRef{
			Container: &pod.Spec.Containers[index],
			Index:     index,
			Type:      RegularContainer,
		}, nil
	}

	return nil, fmt.Errorf("container with name %s not found for fraction request", name)
}
