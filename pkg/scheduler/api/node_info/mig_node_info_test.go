// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
)

func TestIsTaskAllocatable_Mig(t *testing.T) {
	nodeCapacityDifferent := common_info.BuildNode("n2", common_info.BuildResourceList("1000m", "1G"))
	nodeCapacityDifferent.Status.Capacity = common_info.BuildResourceList("2000m", "2G")

	tests := map[string]allocatableTestData{
		"Mig pod on mig node": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithMig("2000m", "2G", "nvidia.com/mig-1g.5gb")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceListWithMig("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb"),
			expected:                true,
			expectedMessageContains: []string{},
		},
		"Mig pod on non-gpu node": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb"),
			expected:                false,
			expectedMessageContains: []string{"nvidia.com/mig-1g.5gb"},
		},
		"Mig pod on non-mig gpu node": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithGPU("2000m", "2G", "10")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb"),
			expected:                false,
			expectedMessageContains: []string{"nvidia.com/mig-1g.5gb"},
		},
		"Mig pod on busy node node - allocatable": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithMig("2000m", "2G", "nvidia.com/mig-1g.5gb", "nvidia.com/mig-1g.5gb")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb")},
			podResourcesToAllocate:  common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb"),
			expected:                true,
			expectedMessageContains: []string{},
		},
		"Mig pod on busy node node - not allocatable": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithMig("2000m", "2G", "nvidia.com/mig-1g.5gb", "nvidia.com/mig-1g.5gb")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb", "nvidia.com/mig-1g.5gb")},
			podResourcesToAllocate:  common_info.BuildResourceListWithMig("1000m", "1G", "nvidia.com/mig-1g.5gb"),
			expected:                false,
			expectedMessageContains: []string{"nvidia.com/mig-1g.5gb"},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			runAllocatableTest(
				t, testData, testName,
				func(ni *NodeInfo, task *pod_info.PodInfo) (bool, *common_info.TasksFitError) {
					allocatable, err := ni.IsTaskAllocatable(task), ni.FittingError(task, false)
					return allocatable, err
				},
			)
		})
	}
}
