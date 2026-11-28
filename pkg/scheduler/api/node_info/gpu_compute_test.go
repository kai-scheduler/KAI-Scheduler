// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const computeTestGpuMemory = int64(100)

func newComputeTestNode(wholeGpus int) *NodeInfo {
	vectorMap := resource_info.NewResourceVectorMap()
	nodeResources := resource_info.NewResource(0, 0, float64(wholeGpus))
	nodeResources.ScalarResources()[resource_info.PodsResourceName] = 10
	return &NodeInfo{
		Name:                   "node0",
		Node:                   common_info.BuildNode("node0", common_info.BuildResourceListWithGPU("8000m", "10G", "2")),
		VectorMap:              vectorMap,
		PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
		MemoryOfEveryGpuOnNode: computeTestGpuMemory,
		GpuMemorySynced:        true,
		GpuSharingNodeInfo:     *newGpuSharingNodeInfo(),
		AllocatableVector:      resource_info.NewResource(0, 0, 2).ToVector(vectorMap),
		IdleVector:             nodeResources.ToVector(vectorMap),
		UsedVector:             resource_info.NewResourceVector(vectorMap),
		ReleasingVector:        resource_info.NewResourceVector(vectorMap),
	}
}

func newComputeTestTask(
	name string, status pod_status.PodStatus, devices int64, portion float64, compute int64, gpuGroups ...string,
) *pod_info.PodInfo {
	vectorMap := resource_info.NewResourceVectorMap()
	task := &pod_info.PodInfo{
		UID:                  common_info.PodID(name),
		Name:                 name,
		Namespace:            "default",
		Status:               status,
		Pod:                  &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}},
		ResourceRequestType:  pod_info.RequestTypeFraction,
		ResourceReceivedType: pod_info.ReceivedTypeFraction,
		VectorMap:            vectorMap,
		ResReqVector:         resource_info.NewResourceVector(vectorMap),
		GpuRequirement:       *resource_info.NewGpuResourceRequirementWithMultiFraction(devices, portion, 0),
	}
	task.GpuRequirement.SetGpuCompute(compute)
	task.SetGPUGroupIDs(gpuGroups)
	return task
}

type computeLedger struct {
	used, allocated, releasing int64
}

func computeLedgerOf(node *NodeInfo, gpuGroup string) computeLedger {
	return computeLedger{
		used:      node.UsedSharedGPUsCompute[gpuGroup],
		allocated: node.AllocatedSharedGPUsCompute[gpuGroup],
		releasing: node.ReleasingSharedGPUsCompute[gpuGroup],
	}
}

func TestGpuComputeLedgerLifecycle(t *testing.T) {
	tests := []struct {
		status   pod_status.PodStatus
		expected computeLedger
	}{
		{status: pod_status.Running, expected: computeLedger{used: 200, allocated: 200, releasing: 0}},
		{status: pod_status.Allocated, expected: computeLedger{used: 200, allocated: 200, releasing: 0}},
		{status: pod_status.Releasing, expected: computeLedger{used: 200, allocated: 200, releasing: 200}},
		// A pipeline reservation is netted against releasing capacity and may make it negative.
		{status: pod_status.Pipelined, expected: computeLedger{used: 200, allocated: 0, releasing: -200}},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			node := newComputeTestNode(2)
			task := newComputeTestTask("task", tt.status, 1, 0.2, 200, "0")

			node.addSharedGPUTaskResources(task)
			require.Equal(t, tt.expected, computeLedgerOf(node, "0"))

			node.removeSharedTaskResources(task)
			require.Equal(t, computeLedger{}, computeLedgerOf(node, "0"))
		})
	}
}

func TestGpuComputeLedgerPerDevice(t *testing.T) {
	node := newComputeTestNode(2)
	task := newComputeTestTask("two-devices", pod_status.Running, 2, 0.2, 200, "0", "1")

	node.addSharedGPUTaskResources(task)

	require.Equal(t, int64(200), node.UsedSharedGPUsCompute["0"])
	require.Equal(t, int64(200), node.UsedSharedGPUsCompute["1"])
	require.Equal(t, int64(200), node.AllocatedSharedGPUsCompute["0"])
	require.Equal(t, int64(200), node.AllocatedSharedGPUsCompute["1"])
}

func TestGpuComputeLedgerMemoryOnlyNeighbor(t *testing.T) {
	node := newComputeTestNode(2)
	memoryOnly := newComputeTestTask("memory-only", pod_status.Running, 1, 0.3, 0, "0")
	computeTask := newComputeTestTask("compute", pod_status.Running, 1, 0.2, 200, "0")
	node.addSharedGPUTaskResources(memoryOnly)
	node.addSharedGPUTaskResources(computeTask)
	idleGpusWithBoth := node.IdleVector.Get(resource_info.GPUIndex)

	node.removeSharedTaskResources(computeTask)

	require.Equal(t, computeLedger{}, computeLedgerOf(node, "0"))
	require.Equal(t, int64(30), node.UsedSharedGPUsMemory["0"])
	require.False(t, node.isAllGpuReleased("0"))
	require.Equal(t, idleGpusWithBoth, node.IdleVector.Get(resource_info.GPUIndex))
}

func TestGpuComputeLedgerClone(t *testing.T) {
	node := newComputeTestNode(2)
	task := newComputeTestTask("task", pod_status.Running, 1, 0.2, 200, "0")
	node.addSharedGPUTaskResources(task)

	clone := node.GpuSharingNodeInfo.Clone()
	clone.UsedSharedGPUsCompute["0"] = 0
	clone.AllocatedSharedGPUsCompute["0"] = 0
	clone.ReleasingSharedGPUsCompute["0"] = 50
	clone.UsedSharedGPUsMemory["0"] = 0

	require.Equal(t, computeLedger{used: 200, allocated: 200}, computeLedgerOf(node, "0"))
	require.Equal(t, int64(20), node.UsedSharedGPUsMemory["0"])
}

func TestGpuComputePreservedInAcceptedRequirement(t *testing.T) {
	node := newComputeTestNode(2)
	task := newComputeTestTask("two-devices", pod_status.Running, 2, 0.2, 200, "0", "1")
	quotaBefore := task.GpuRequirement.GetGpusQuota()

	node.setAcceptedResources(task)

	require.Equal(t, int64(200), task.GpuRequirement.Clone().GpuCompute())
	require.Equal(t, int64(200), task.AcceptedGpuRequirement.GpuCompute())
	require.Equal(t, int64(2), task.AcceptedGpuRequirement.GetNumOfGpuDevices())
	require.Equal(t, quotaBefore, task.AcceptedGpuRequirement.GetGpusQuota())
}

func setComputeTestGroup(node *NodeInfo, gpuGroup string, allocatedMemory, allocatedCompute, releasingCompute int64) {
	node.UsedSharedGPUsMemory[gpuGroup] = allocatedMemory
	node.AllocatedSharedGPUsMemory[gpuGroup] = allocatedMemory
	node.UsedSharedGPUsCompute[gpuGroup] = allocatedCompute
	node.AllocatedSharedGPUsCompute[gpuGroup] = allocatedCompute
	node.ReleasingSharedGPUsCompute[gpuGroup] = releasingCompute
}

func TestGpuComputeFit(t *testing.T) {
	tests := []struct {
		name             string
		allocated        int64
		releasing        int64
		request          int64
		expectedGroupFit bool
		expectedIdleFit  bool
	}{
		{name: "exactly fills the GPU", allocated: 700, request: 300, expectedGroupFit: true, expectedIdleFit: true},
		{name: "one unit over capacity", allocated: 1000, request: 1, expectedGroupFit: false, expectedIdleFit: false},
		{name: "memory-only on a full GPU", allocated: 1000, request: 0, expectedGroupFit: true, expectedIdleFit: true},
		{name: "fits only on releasing", allocated: 1000, releasing: 300, request: 200, expectedGroupFit: true, expectedIdleFit: false},
		{name: "releasing already reserved by pipeline", allocated: 1000, releasing: 100, request: 101, expectedGroupFit: false, expectedIdleFit: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newComputeTestNode(0)
			setComputeTestGroup(node, "0", 20, tt.allocated, tt.releasing)
			task := newComputeTestTask("pending", pod_status.Pending, 1, 0.2, tt.request)

			require.Equal(t, tt.expectedGroupFit, node.IsTaskFitOnGpuGroup(task, "0"))
			require.Equal(t, tt.expectedIdleFit, node.EnoughIdleResourcesOnGpu(&task.GpuRequirement, "0"))
		})
	}
}

func TestGpuComputeFitKeepsMemoryRejection(t *testing.T) {
	node := newComputeTestNode(0)
	setComputeTestGroup(node, "0", 90, 0, 0)
	task := newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 100)

	require.False(t, node.IsTaskFitOnGpuGroup(task, "0"))
	require.False(t, node.EnoughIdleResourcesOnGpu(&task.GpuRequirement, "0"))
}

func TestGpuComputeFitAfterPipelineOntoReleasing(t *testing.T) {
	node := newComputeTestNode(0)
	setComputeTestGroup(node, "0", 20, 1000, 300)
	pipelined := newComputeTestTask("pipelined", pod_status.Pipelined, 1, 0.2, 200, "0")
	node.addSharedGPUTaskResources(pipelined)

	require.Equal(t, int64(100), node.AvailableSharedGpuCompute("0"))
	require.False(t, node.IsTaskFitOnGpuGroup(newComputeTestTask("next", pod_status.Pending, 1, 0.2, 101), "0"))
	require.True(t, node.IsTaskFitOnGpuGroup(newComputeTestTask("next", pod_status.Pending, 1, 0.2, 100), "0"))
}

func TestGpuComputeFitError(t *testing.T) {
	t.Run("insufficient compute on the only eligible GPU", func(t *testing.T) {
		node := newComputeTestNode(0)
		setComputeTestGroup(node, "0", 20, 900, 0)

		fitErr := node.GpuComputeFitError(newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 200))

		require.NotNil(t, fitErr)
		require.Equal(t, []string{
			"node node0 has enough GPU memory but not enough GPU compute (requested 0.2, free 0.1)",
		}, fitErr.Reasons)
	})

	t.Run("smallest unit is rendered", func(t *testing.T) {
		node := newComputeTestNode(0)
		setComputeTestGroup(node, "0", 20, 1000, 0)

		fitErr := node.GpuComputeFitError(newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 1))

		require.NotNil(t, fitErr)
		require.Equal(t, []string{
			"node node0 has enough GPU memory but not enough GPU compute (requested 0.001, free 0)",
		}, fitErr.Reasons)
	})

	t.Run("reported through FittingError", func(t *testing.T) {
		node := newComputeTestNode(0)
		setComputeTestGroup(node, "0", 20, 900, 0)

		fitErr := node.FittingError(newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 200), false)

		require.NotNil(t, fitErr)
		require.Equal(t, []string{
			"node node0 has enough GPU memory but not enough GPU compute (requested 0.2, free 0.1)",
		}, fitErr.Reasons)
	})

	noErrorCases := []struct {
		name  string
		setup func(node *NodeInfo)
		task  *pod_info.PodInfo
	}{
		{
			name:  "memory-only pod",
			setup: func(node *NodeInfo) { setComputeTestGroup(node, "0", 20, 1000, 0) },
			task:  newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 0),
		},
		{
			name:  "memory is the bottleneck",
			setup: func(node *NodeInfo) { setComputeTestGroup(node, "0", 90, 900, 0) },
			task:  newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 200),
		},
		{
			name: "another eligible GPU has enough compute",
			setup: func(node *NodeInfo) {
				setComputeTestGroup(node, "0", 20, 900, 0)
				setComputeTestGroup(node, "1", 20, 500, 0)
			},
			task: newComputeTestTask("pending", pod_status.Pending, 1, 0.2, 200),
		},
	}
	for _, tt := range noErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			node := newComputeTestNode(0)
			tt.setup(node)
			require.Nil(t, node.GpuComputeFitError(tt.task))
		})
	}
}

func TestGpuComputeFitErrorMultiDevice(t *testing.T) {
	node := newComputeTestNode(0)
	setComputeTestGroup(node, "0", 20, 400, 0)
	setComputeTestGroup(node, "1", 20, 800, 0)
	task := newComputeTestTask("two-devices", pod_status.Pending, 2, 0.2, 300)

	fitErr := node.GpuComputeFitError(task)
	require.NotNil(t, fitErr)
	require.Equal(t, []string{
		"node node0 has enough GPU memory but not enough GPU compute (requested 0.3, free 0.2)",
	}, fitErr.Reasons)

	setComputeTestGroup(node, "2", 20, 600, 0)
	require.Nil(t, node.GpuComputeFitError(task))
}
