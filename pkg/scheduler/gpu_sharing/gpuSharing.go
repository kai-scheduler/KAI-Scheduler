// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpu_sharing

import (
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type nodeGpuForSharing struct {
	Groups      []string
	IsReleasing bool
}

func AllocateFractionalGPUTaskToNode(ssn *framework.Session, stmt *framework.Statement, pod *pod_info.PodInfo,
	node *node_info.NodeInfo, isPipelineOnly bool) bool {
	fittingGPUs := ssn.FittingGPUs(node, pod)
	gpuForSharing := GetNodePreferableGpuForSharing(fittingGPUs, node, pod, isPipelineOnly)
	if gpuForSharing == nil {
		return false
	}

	pod.GPUGroups = gpuForSharing.Groups

	isPipelineOnly = isPipelineOnly || gpuForSharing.IsReleasing
	success := allocateSharedGPUTask(ssn, stmt, node, pod, isPipelineOnly)
	if !success {
		pod.GPUGroups = nil
	}
	return success
}

func GetNodePreferableGpuForSharing(fittingGPUsOnNode []string, node *node_info.NodeInfo, pod *pod_info.PodInfo,
	isPipelineOnly bool) *nodeGpuForSharing {

	nodeGpusSharing := &nodeGpuForSharing{
		Groups:      []string{},
		IsReleasing: false,
	}

	deviceCounts := pod.ResReq.GetNumOfGpuDevices()
	for _, gpuIdx := range fittingGPUsOnNode {
		if gpuIdx == pod_info.WholeGpuIndicator {
			if wholeGpuForSharing := findGpuForSharingOnNode(pod, node, isPipelineOnly); wholeGpuForSharing != nil {
				nodeGpusSharing.IsReleasing =
					nodeGpusSharing.IsReleasing || wholeGpuForSharing.IsReleasing
				nodeGpusSharing.Groups = append(nodeGpusSharing.Groups, wholeGpuForSharing.Groups...)
			}
		} else {
			nodeGpusSharing.IsReleasing =
				nodeGpusSharing.IsReleasing ||
					!node.EnoughIdleResourcesOnGpu(pod.ResReq, gpuIdx) ||
					!node.IsTaskAllocatable(pod)
			nodeGpusSharing.Groups = append(nodeGpusSharing.Groups, gpuIdx)
		}

		if len(nodeGpusSharing.Groups) == int(deviceCounts) {
			return nodeGpusSharing
		}
	}

	return nil
}

func findGpuForSharingOnNode(task *pod_info.PodInfo, node *node_info.NodeInfo, isPipelineOnly bool) *nodeGpuForSharing {
	isReleasing := true
	if !isPipelineOnly {
		if taskAllocatable := node.IsTaskAllocatable(task); taskAllocatable {
			isReleasing = false
		}
	}
	return &nodeGpuForSharing{Groups: []string{string(uuid.NewUUID())}, IsReleasing: isReleasing}
}

func allocateSharedGPUTask(ssn *framework.Session, stmt *framework.Statement, node *node_info.NodeInfo,
	task *pod_info.PodInfo, isPipelineOnly bool) bool {
	if isPipelineOnly {
		log.InfraLogger.V(6).Infof(
			"Pipelining Task <%v/%v> to node <%v> gpuGroup: <%v>, requires: <%v, %v mb> GPUs",
			task.Namespace, task.Name, node.Name,
			task.GPUGroups, task.ResReq.GPUs(), task.ResReq.GpuMemory())
		if err := stmt.Pipeline(task, node.Name, !isPipelineOnly); err != nil {
			log.InfraLogger.V(6).Infof("Failed to pipeline Task: <%s/%s> on Node: <%s>, due to an error: %v",
				task.Namespace, task.Name, node.Name, err)
			return false
		}

		return true
	}

	if err := stmt.Allocate(task, node.Name); err != nil {
		log.InfraLogger.Errorf("Failed to bind Task <%v> on <%v> in Session <%v>, err: <%v>",
			task.UID, node.Name, ssn.ID, err)
		return false
	}

	return true
}
