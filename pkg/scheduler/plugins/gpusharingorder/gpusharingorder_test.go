// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingorder

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

// A node with one shared GPU that has half its memory in use and nothing releasing.
func nodeWithUsedSharedGpu() *node_info.NodeInfo {
	return &node_info.NodeInfo{
		Name:                   "n1",
		MemoryOfEveryGpuOnNode: node_info.DefaultGpuMemory,
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			ReleasingSharedGPUs:       map[string]bool{},
			UsedSharedGPUsMemory:      map[string]int64{"0": node_info.DefaultGpuMemory / 2},
			ReleasingSharedGPUsMemory: map[string]int64{},
			AllocatedSharedGPUsMemory: map[string]int64{"0": node_info.DefaultGpuMemory / 2},
		},
	}
}

func TestNodeOrderFn(t *testing.T) {
	plugin := &gpuSharingOrderPlugin{}
	node := nodeWithUsedSharedGpu()

	fractionPod := &pod_info.PodInfo{
		ResourceRequestType: pod_info.RequestTypeFraction,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(0.5, 0),
	}
	score, err := plugin.nodeOrderFn(fractionPod, node)
	if err != nil || score != scores.GpuSharing {
		t.Fatalf("fraction pod: expected score %v on a node with a shared GPU it fits, got %v (err %v)", scores.GpuSharing, score, err)
	}

	cpuOnlyPod := &pod_info.PodInfo{
		ResourceRequestType: pod_info.RequestTypeRegular,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(0, 0),
	}
	score, err = plugin.nodeOrderFn(cpuOnlyPod, node)
	if err != nil || score != 0 {
		t.Fatalf("cpu-only pod: expected score 0 on a node with shared GPUs, got %v (err %v)", score, err)
	}

	wholeGpuPod := &pod_info.PodInfo{
		ResourceRequestType: pod_info.RequestTypeRegular,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(1, 0),
	}
	score, err = plugin.nodeOrderFn(wholeGpuPod, node)
	if err != nil || score != 0 {
		t.Fatalf("whole-gpu pod: expected score 0 on a node with shared GPUs, got %v (err %v)", score, err)
	}
}
