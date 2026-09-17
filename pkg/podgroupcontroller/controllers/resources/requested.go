// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	common_resources "github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

const (
	gpuMemoryResourceName = "run.ai/gpu.memory"
)

func ExtractGPUSharingRequestedResources(pod *v1.Pod) (v1.ResourceList, error) {
	resources := v1.ResourceList{}

	req, err := common_resources.ParsePodGPUFractionRequest(pod)
	if err != nil {
		return v1.ResourceList{},
			fmt.Errorf("failed to parse GPU fraction for pod %s/%s: %s", pod.Namespace, pod.Name, err)
	}
	if req == nil {
		return resources, nil
	}

	if req.Portion > 0 {
		fractionStr := fmt.Sprintf("%g", req.Portion)
		quantity, err := resource.ParseQuantity(fractionStr)
		if err != nil {
			return v1.ResourceList{},
				fmt.Errorf("failed to parse gpu fraction value <%s>: %s", fractionStr, err)
		}
		if ok := quantity.Mul(req.NumDevices); !ok {
			return v1.ResourceList{},
				fmt.Errorf("failed to multiply gpu fraction by device count. fraction <%s>, count: %d",
					fractionStr, req.NumDevices)
		}
		resources[v1.ResourceName(constants.NvidiaGpuResource)] = quantity
	}

	if req.Memory != nil {
		quantity := req.Memory.DeepCopy()
		if ok := quantity.Mul(req.NumDevices); !ok {
			return v1.ResourceList{},
				fmt.Errorf("failed to multiply gpu memory by device count. memory <%s>, count: %d",
					req.Memory.String(), req.NumDevices)
		}
		resources[v1.ResourceName(gpuMemoryResourceName)] = quantity
	}

	return resources, nil
}
