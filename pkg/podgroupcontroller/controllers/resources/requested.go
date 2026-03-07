// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

const (
	gpuMemoryResourceName = "run.ai/gpu.memory"
)

func ExtractGPUSharingRequestedResources(pod *v1.Pod) (v1.ResourceList, error) {
	resources := v1.ResourceList{}

	fractionsCount := int64(1)
	gpuFractionsCountStr, hasAnnotation := pod.Annotations[constants.GpuFractionsNumDevices]
	if hasAnnotation {
		quantity, err := resource.ParseQuantity(gpuFractionsCountStr)
		if err != nil {
			return v1.ResourceList{},
				fmt.Errorf("failed to parse gpu fraction count annotation value <%s>, error: %s",
					gpuFractionsCountStr, err.Error())
		}
		var successfulIntExtraction bool
		fractionsCount, successfulIntExtraction = quantity.AsInt64()
		if !successfulIntExtraction {
			return v1.ResourceList{},
				fmt.Errorf("failed to extract int value from gpu fraction count annotation. value <%s>",
					gpuFractionsCountStr)
		}
	}

	gpuFractionStr, hasAnnotation := pod.Annotations[constants.GpuFraction]
	if hasAnnotation {
		quantity, err := resource.ParseQuantity(gpuFractionStr)
		if err != nil {
			return v1.ResourceList{},
				fmt.Errorf("failed to parse gpu fraction annotation value <%s>, error: %s",
					gpuFractionStr, err.Error())
		}
		successfulMulti := quantity.Mul(fractionsCount)
		if !successfulMulti {
			return v1.ResourceList{},
				fmt.Errorf("failed to multiple gpu fraction by the fraction count. "+
					"Please check resource.Quantity restrictions. fraction <%s>, count: %d",
					gpuFractionStr, fractionsCount)
		}
		resources[v1.ResourceName(constants.NvidiaGpuResource)] = quantity
	}

	gpuMemoryStr, hasAnnotation := pod.Annotations[constants.GpuMemory]
	if hasAnnotation {
		quantity, err := resource.ParseQuantity(gpuMemoryStr)
		if err != nil {
			return v1.ResourceList{},
				fmt.Errorf("failed to parse gpu memory annotation value <%s>, error: %s",
					gpuMemoryStr, err.Error())
		}
		successfulMulti := quantity.Mul(fractionsCount)
		if !successfulMulti {
			return v1.ResourceList{},
				fmt.Errorf("failed to multiple gpu memory by the fraction count. "+
					"Please check resource.Quantity restrictions. fraction <%s>, count: %d",
					gpuMemoryStr, fractionsCount)
		}
		resources[v1.ResourceName(gpuMemoryResourceName)] = quantity
	}

	return resources, nil
}
