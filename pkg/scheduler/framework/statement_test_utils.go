// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
	taskResReqVector resource_info.ResourceVector, taskVectorMap *resource_info.ResourceVectorMap) {
	if originalNodeAssertData != nil {
		actualNode := extractNodeAssertedInfo(nodeInfo)

		assert.Equal(t, originalNodeAssertData.releasing.Get(resource_info.GPUIndex),
			actualNode.releasing.Get(resource_info.GPUIndex)-taskResReqVector.Get(resource_info.GPUIndex))
		assert.Equal(t, originalNodeAssertData.releasing.Get(resource_info.MemoryIndex),
			actualNode.releasing.Get(resource_info.MemoryIndex)-taskResReqVector.Get(resource_info.MemoryIndex))
		assert.Equal(t, originalNodeAssertData.releasing.Get(resource_info.CPUIndex),
			actualNode.releasing.Get(resource_info.CPUIndex)-taskResReqVector.Get(resource_info.CPUIndex))

		assert.Equal(t, originalNodeAssertData.used, actualNode.used)
		assert.Equal(t, originalNodeAssertData.idle, actualNode.idle)
	}
}

func validateEvictedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	task *pod_info.PodInfo, originalJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation resource_info.ResourceVector) {
	actualAllocated := ssn.ClusterInfo.PodGroupInfos[jobName].AllocatedVector.Clone()
	gpuIdx := resource_info.GPUIndex

	if pod_status.AllocatedStatus(task.Status) {
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx),
			actualAllocated.Get(gpuIdx)+task.ResReqVector.Get(gpuIdx))
		actualAllocated.Add(task.ResReqVector)
		assert.Equal(t, originalJob.AllocatedVector, actualAllocated)
	} else {
		assert.Equal(t, originalJob.AllocatedVector, actualAllocated)
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx), actualAllocated.Get(gpuIdx))
	}
}

func validateEvictedTask(t *testing.T, ssn *Session,
	jobName common_info.PodGroupID, podName common_info.PodID, originalTask *pod_info.PodInfo) {
	actualTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, originalTask.GpuRequirement, actualTask.GpuRequirement)
	assert.Equal(t, originalTask.ResReqVector, actualTask.ResReqVector)

	if pod_status.AllocatedStatus(originalTask.Status) {
		assert.Equal(t, pod_status.Releasing, actualTask.Status)
	} else {
		assert.Equal(t, originalTask.Status, actualTask.Status)
	}
}

func validatePipelinedTask(t *testing.T, ssn *Session, jobName common_info.PodGroupID, podName common_info.PodID,
	nodeToPipeline string, originalPipelinedTask *pod_info.PodInfo) {
	actualOnEvictTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, originalPipelinedTask.GpuRequirement, actualOnEvictTask.GpuRequirement)
	assert.Equal(t, originalPipelinedTask.ResReqVector, actualOnEvictTask.ResReqVector)
	assert.Equal(t, pod_status.Pipelined, actualOnEvictTask.Status)
	assert.Equal(t, nodeToPipeline, actualOnEvictTask.NodeName)
}

func validatePipelinedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	originalTask *pod_info.PodInfo, originalJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation resource_info.ResourceVector) {
	actualAllocated := ssn.ClusterInfo.PodGroupInfos[jobName].AllocatedVector.Clone()
	gpuIdx := resource_info.GPUIndex

	if pod_status.AllocatedStatus(originalTask.Status) {
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx),
			actualAllocated.Get(gpuIdx)+originalTask.ResReqVector.Get(gpuIdx))
		actualAllocated.Add(originalTask.ResReqVector)
		assert.Equal(t, originalJob.AllocatedVector, actualAllocated)
	} else {
		assert.Equal(t, originalJob.AllocatedVector, actualAllocated)
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx), actualAllocated.Get(gpuIdx))
	}
}

func validatePipelinedToNode(t *testing.T,
	nodeInfo *node_info.NodeInfo, targetNodeOriginalData *nodeAssertedInfo,
	taskResReqVector resource_info.ResourceVector, taskVectorMap *resource_info.ResourceVectorMap) {
	actualNode := extractNodeAssertedInfo(nodeInfo)
	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.GPUIndex),
		actualNode.used.Get(resource_info.GPUIndex)-taskResReqVector.Get(resource_info.GPUIndex))
	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.MemoryIndex),
		actualNode.used.Get(resource_info.MemoryIndex)-taskResReqVector.Get(resource_info.MemoryIndex))
	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.CPUIndex),
		actualNode.used.Get(resource_info.CPUIndex)-taskResReqVector.Get(resource_info.CPUIndex))

	assert.Equal(t, targetNodeOriginalData.idle, actualNode.idle)
}

func validateAllocatedTask(t *testing.T, ssn *Session, jobName common_info.PodGroupID, podName common_info.PodID,
	nodeToAllocate string, originalPipelinedTask *pod_info.PodInfo) {
	actualTask := ssn.ClusterInfo.PodGroupInfos[jobName].GetAllPodsMap()[podName]

	assert.Equal(t, originalPipelinedTask.GpuRequirement, actualTask.GpuRequirement)
	assert.Equal(t, originalPipelinedTask.ResReqVector, actualTask.ResReqVector)
	assert.Equal(t, pod_status.Allocated, actualTask.Status)
	assert.Equal(t, nodeToAllocate, actualTask.NodeName)
}

func validateAllocatedJob(t *testing.T, ssn *Session, jobName common_info.PodGroupID,
	originalAllocateTask *pod_info.PodInfo, originalAllocateJob *podgroup_info.PodGroupInfo,
	expectedJobAllocation resource_info.ResourceVector) {
	actualAllocated := ssn.ClusterInfo.PodGroupInfos[jobName].AllocatedVector.Clone()
	gpuIdx := resource_info.GPUIndex

	if !pod_status.AllocatedStatus(originalAllocateTask.Status) {
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx),
			originalAllocateJob.AllocatedVector.Get(gpuIdx)+originalAllocateTask.ResReqVector.Get(gpuIdx))
		originalAllocateJob.AllocatedVector.Add(originalAllocateTask.ResReqVector)
		assert.Equal(t, actualAllocated, originalAllocateJob.AllocatedVector)
	} else {
		assert.Equal(t, originalAllocateJob.AllocatedVector, actualAllocated)
		assert.Equal(t, expectedJobAllocation.Get(gpuIdx), actualAllocated.Get(gpuIdx))
	}
}

func validateAllocatedToNode(t *testing.T,
	nodeInfo *node_info.NodeInfo, targetNodeOriginalData *nodeAssertedInfo,
	taskResReqVector resource_info.ResourceVector, taskVectorMap *resource_info.ResourceVectorMap) {
	actualNode := extractNodeAssertedInfo(nodeInfo)
	assert.Equal(t, targetNodeOriginalData.idle.Get(resource_info.GPUIndex), actualNode.idle.Get(resource_info.GPUIndex)+taskResReqVector.Get(resource_info.GPUIndex))
	assert.Equal(t, targetNodeOriginalData.idle.Get(resource_info.MemoryIndex), actualNode.idle.Get(resource_info.MemoryIndex)+taskResReqVector.Get(resource_info.MemoryIndex))
	assert.Equal(t, targetNodeOriginalData.idle.Get(resource_info.CPUIndex), actualNode.idle.Get(resource_info.CPUIndex)+taskResReqVector.Get(resource_info.CPUIndex))

	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.GPUIndex), actualNode.used.Get(resource_info.GPUIndex)-taskResReqVector.Get(resource_info.GPUIndex))
	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.MemoryIndex), actualNode.used.Get(resource_info.MemoryIndex)-taskResReqVector.Get(resource_info.MemoryIndex))
	assert.Equal(t, targetNodeOriginalData.used.Get(resource_info.CPUIndex), actualNode.used.Get(resource_info.CPUIndex)-taskResReqVector.Get(resource_info.CPUIndex))

	assert.Equal(t, targetNodeOriginalData.releasing, actualNode.releasing)
}

func buildGpuVector(vectorMap *resource_info.ResourceVectorMap, gpus float64) resource_info.ResourceVector {
	vec := resource_info.NewResourceVector(vectorMap)
	vec.Set(resource_info.GPUIndex, gpus)
	return vec
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
