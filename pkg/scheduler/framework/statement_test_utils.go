// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	storagecapacity "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
)

type nodeAssertedInfo struct {
	idle                        resource_info.ResourceVector
	used                        resource_info.ResourceVector
	allocatable                 resource_info.ResourceVector
	releasing                   resource_info.ResourceVector
	vectorMap                   *resource_info.ResourceVectorMap
	accessibleStorageCapacities map[common_info.StorageClassID][]*storagecapacity.StorageCapacityInfo
	gpuSharingNodeInfo          *node_info.GpuSharingNodeInfo
}

func (nri *nodeAssertedInfo) assertEqual(t *testing.T, nriRight *nodeAssertedInfo) {
	assert.Equal(t, nri.idle, nriRight.idle)
	assert.Equal(t, nri.used, nriRight.used)
	assert.Equal(t, nri.allocatable, nriRight.allocatable)
	assert.Equal(t, nri.releasing, nriRight.releasing)
	assert.Equal(t, nri.accessibleStorageCapacities, nriRight.accessibleStorageCapacities)
	assert.Equal(t, nri.gpuSharingNodeInfo.ReleasingSharedGPUs, nriRight.gpuSharingNodeInfo.ReleasingSharedGPUs)
	assert.Equal(t, nri.gpuSharingNodeInfo.UsedSharedGPUsMemory, nriRight.gpuSharingNodeInfo.UsedSharedGPUsMemory)
	nri.assertSharedGpuReleasingMapEqual(t, nriRight.gpuSharingNodeInfo.ReleasingSharedGPUsMemory)
	assert.Equal(t, nri.gpuSharingNodeInfo.AllocatedSharedGPUsMemory, nriRight.gpuSharingNodeInfo.AllocatedSharedGPUsMemory)
}

func (nri *nodeAssertedInfo) assertSharedGpuReleasingMapEqual(t *testing.T, releasingSharedGPUsMemory map[string]int64) {
	for leftKey, leftValue := range nri.gpuSharingNodeInfo.ReleasingSharedGPUsMemory {
		if leftValue != 0 {
			assert.Contains(t, releasingSharedGPUsMemory, leftKey)
			assert.Equal(t, leftValue, releasingSharedGPUsMemory[leftKey])
		}
		rightValue, hasKey := releasingSharedGPUsMemory[leftKey]
		if hasKey {
			assert.Equal(t, leftValue, rightValue)
		}
	}
	for rightKey, rightValue := range releasingSharedGPUsMemory {
		if rightValue != 0 {
			assert.Contains(t, nri.gpuSharingNodeInfo.ReleasingSharedGPUsMemory, rightKey)
			assert.Equal(t, nri.gpuSharingNodeInfo.ReleasingSharedGPUsMemory[rightKey], rightValue)
		}
		leftValue, hasKey := nri.gpuSharingNodeInfo.ReleasingSharedGPUsMemory[rightKey]
		if hasKey {
			assert.Equal(t, leftValue, rightValue)
		}
	}
}

func validateEvictedFromNode(t *testing.T,
	nodeInfo *node_info.NodeInfo, originalNodeAssertData *nodeAssertedInfo,
	taskResources *resource_info.ResourceRequirements) {
	if originalNodeAssertData != nil {
		actualNode := extractNodeAssertedInfo(nodeInfo)
		vm := actualNode.vectorMap
		gpuIdx := vm.GetIndex("gpu")
		cpuIdx := vm.GetIndex(v1.ResourceCPU)
		memIdx := vm.GetIndex(v1.ResourceMemory)

		assert.Equal(t, originalNodeAssertData.releasing.Get(gpuIdx),
			actualNode.releasing.Get(gpuIdx)-taskResources.GPUs())
		assert.Equal(t, originalNodeAssertData.releasing.Get(memIdx),
			actualNode.releasing.Get(memIdx)-taskResources.Memory())
		assert.Equal(t, originalNodeAssertData.releasing.Get(cpuIdx),
			actualNode.releasing.Get(cpuIdx)-taskResources.Cpu())

		assert.Equal(t, originalNodeAssertData.used, actualNode.used)
		assert.Equal(t, originalNodeAssertData.idle, actualNode.idle)
	}
}

func validateEvictedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	task *pod_info.PodInfo, originalJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation *resource_info.Resource) {
	actualAllocated := (*ssn.ClusterInfo.PodGroupInfos[jobName].Allocated).Clone()

	if pod_status.AllocatedStatus(task.Status) {
		assert.Equal(t, expectedJobAllocation.GPUs(),
			actualAllocated.GPUs()+(*task.ResReq).GPUs())
		actualAllocated.AddResourceRequirements(task.ResReq)
		assert.Equal(t, *originalJob.Allocated,
			*actualAllocated)
	} else {
		assert.Equal(t, *originalJob.Allocated, *actualAllocated)
		assert.Equal(t, expectedJobAllocation.GPUs(), actualAllocated.GPUs())
	}
}

func validateEvictedTask(t *testing.T, ssn *Session,
	jobName common_info.PodGroupID, podName common_info.PodID, originalTask *pod_info.PodInfo) {
	actualTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, *originalTask.ResReq, *actualTask.ResReq)

	if pod_status.AllocatedStatus(originalTask.Status) {
		assert.Equal(t, pod_status.Releasing, actualTask.Status)
	} else {
		assert.Equal(t, originalTask.Status, actualTask.Status)
	}
}

func validatePipelinedTask(t *testing.T, ssn *Session, jobName common_info.PodGroupID, podName common_info.PodID,
	nodeToPipeline string, originalPipelinedTask *pod_info.PodInfo) {
	actualOnEvictTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, *originalPipelinedTask.ResReq, *actualOnEvictTask.ResReq)
	assert.Equal(t, pod_status.Pipelined, actualOnEvictTask.Status)
	assert.Equal(t, nodeToPipeline, actualOnEvictTask.NodeName)
}

func validatePipelinedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	originalTask *pod_info.PodInfo, originalJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation *resource_info.Resource) {
	actualAllocated := (*ssn.ClusterInfo.PodGroupInfos[jobName].Allocated).Clone()

	if pod_status.AllocatedStatus(originalTask.Status) {
		assert.Equal(t, expectedJobAllocation.GPUs(),
			actualAllocated.GPUs()+(*originalTask.ResReq).GPUs())
		actualAllocated.AddResourceRequirements(originalTask.ResReq)
		assert.Equal(t, *originalJob.Allocated,
			*actualAllocated)
	} else {
		assert.Equal(t, *originalJob.Allocated, *actualAllocated)
		assert.Equal(t, expectedJobAllocation.GPUs(), actualAllocated.GPUs())
	}
}

func validatePipelinedToNode(t *testing.T,
	nodeInfo *node_info.NodeInfo, targetNodeOriginalData *nodeAssertedInfo,
	taskResources *resource_info.ResourceRequirements) {
	actualNode := extractNodeAssertedInfo(nodeInfo)
	vm := actualNode.vectorMap
	gpuIdx := vm.GetIndex("gpu")
	cpuIdx := vm.GetIndex(v1.ResourceCPU)
	memIdx := vm.GetIndex(v1.ResourceMemory)

	assert.Equal(t, targetNodeOriginalData.used.Get(gpuIdx),
		actualNode.used.Get(gpuIdx)-taskResources.GPUs())
	assert.Equal(t, targetNodeOriginalData.used.Get(memIdx),
		actualNode.used.Get(memIdx)-taskResources.Memory())
	assert.Equal(t, targetNodeOriginalData.used.Get(cpuIdx),
		actualNode.used.Get(cpuIdx)-taskResources.Cpu())

	assert.Equal(t, targetNodeOriginalData.idle, actualNode.idle)
}

func validateAllocatedTask(t *testing.T, ssn *Session, jobName common_info.PodGroupID, podName common_info.PodID,
	nodeToAllocate string, originalPipelinedTask *pod_info.PodInfo) {
	actualTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, *originalPipelinedTask.ResReq, *actualTask.ResReq)
	assert.Equal(t, pod_status.Allocated, actualTask.Status)
	assert.Equal(t, nodeToAllocate, actualTask.NodeName)
}

func validateAllocatedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	originalAllocateTask *pod_info.PodInfo, originalAllocateJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation *resource_info.Resource) {
	actualAllocated := (*ssn.ClusterInfo.PodGroupInfos[jobName].Allocated).Clone()

	if !pod_status.AllocatedStatus(originalAllocateTask.Status) {
		assert.Equal(t, expectedJobAllocation.GPUs(),
			originalAllocateJob.Allocated.GPUs()+(*originalAllocateTask.ResReq).GPUs())
		originalAllocateJob.Allocated.AddResourceRequirements(originalAllocateTask.ResReq)
		assert.Equal(t, *actualAllocated, *originalAllocateJob.Allocated)
	} else {
		assert.Equal(t, *originalAllocateJob.Allocated, *actualAllocated)
		assert.Equal(t, expectedJobAllocation.GPUs(), actualAllocated.GPUs())
	}
}

func validateAllocatedToNode(t *testing.T,
	nodeInfo *node_info.NodeInfo, targetNodeOriginalData *nodeAssertedInfo,
	taskResources *resource_info.ResourceRequirements) {
	actualNode := extractNodeAssertedInfo(nodeInfo)
	vm := actualNode.vectorMap
	gpuIdx := vm.GetIndex("gpu")
	cpuIdx := vm.GetIndex(v1.ResourceCPU)
	memIdx := vm.GetIndex(v1.ResourceMemory)

	assert.Equal(t, targetNodeOriginalData.idle.Get(gpuIdx), actualNode.idle.Get(gpuIdx)+taskResources.GPUs())
	assert.Equal(t, targetNodeOriginalData.idle.Get(memIdx), actualNode.idle.Get(memIdx)+taskResources.Memory())
	assert.Equal(t, targetNodeOriginalData.idle.Get(cpuIdx), actualNode.idle.Get(cpuIdx)+taskResources.Cpu())

	assert.Equal(t, targetNodeOriginalData.used.Get(gpuIdx), actualNode.used.Get(gpuIdx)-taskResources.GPUs())
	assert.Equal(t, targetNodeOriginalData.used.Get(memIdx), actualNode.used.Get(memIdx)-taskResources.Memory())
	assert.Equal(t, targetNodeOriginalData.used.Get(cpuIdx), actualNode.used.Get(cpuIdx)-taskResources.Cpu())

	assert.Equal(t, targetNodeOriginalData.releasing, actualNode.releasing)
}

func extractNodeAssertedInfo(nodeInfo *node_info.NodeInfo) *nodeAssertedInfo {
	assertedInfo := nodeAssertedInfo{}
	assertedInfo.idle = nodeInfo.IdleVector.Clone()
	assertedInfo.used = nodeInfo.UsedVector.Clone()
	assertedInfo.allocatable = nodeInfo.AllocatableVector.Clone()
	assertedInfo.releasing = nodeInfo.ReleasingVector.Clone()
	assertedInfo.vectorMap = nodeInfo.VectorMap

	assertedInfo.accessibleStorageCapacities =
		map[common_info.StorageClassID][]*storagecapacity.StorageCapacityInfo{}
	for k, v := range nodeInfo.AccessibleStorageCapacities {
		assertedInfo.accessibleStorageCapacities[k] = v
	}
	assertedInfo.gpuSharingNodeInfo = nodeInfo.GpuSharingNodeInfo.Clone()
	return &assertedInfo
}
