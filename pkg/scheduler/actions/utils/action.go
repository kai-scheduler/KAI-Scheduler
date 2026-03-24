// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func GetVictimsQueue(
	ssn *framework.Session,
	filter func(*podgroup_info.PodGroupInfo) bool) *JobsOrderByQueues {
	preemptees := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}

	for _, job := range ssn.PodGroupInfos {
		atLeastOneAlivePod := false
		for _, task := range job.PodInfos {
			if !pod_status.IsAliveStatus(task.Status) {
				continue
			}
			if task.NodeName != "" {
				if _, found := ssn.Nodes[task.NodeName]; !found {
					log.InfraLogger.Errorf("Failed to find node for task: <%v,%v> ", task.Namespace, task.Name)
					continue
				}
			}
			atLeastOneAlivePod = true
		}
		if atLeastOneAlivePod && (filter == nil || filter(job)) {
			preemptees[job.UID] = job
		}
	}
	victimsQueue := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
		VictimQueue:       true,
		MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite,
	})
	victimsQueue.InitializeWithJobs(preemptees)
	return &victimsQueue
}

func GetMessageOfEviction(ssn *framework.Session, actionType framework.ActionType, preempteeTask *pod_info.PodInfo,
	preemptorJob *podgroup_info.PodGroupInfo) string {
	switch actionType {
	case framework.Preempt:
		return api.GetPreemptMessage(preemptorJob, preempteeTask)

	case framework.Consolidation:
		return api.GetConsolidateMessage(preempteeTask)

	case framework.Reclaim:
		preempteeJob, found := ssn.PodGroupInfos[preempteeTask.Job]
		if !found {
			log.InfraLogger.Errorf(
				"Failed to get preemptee job for task: <%s/%s>", preempteeTask.Namespace, preempteeTask.Name)
			return ""
		}
		reclaimerQueue := ssn.Queues[preemptorJob.Queue]
		reclaimerParentQueue := ssn.Queues[reclaimerQueue.ParentQueue]
		reclaimeeQueue := ssn.Queues[preempteeJob.Queue]
		reclaimeeParentQueue := ssn.Queues[reclaimeeQueue.ParentQueue]

		msg := api.GetReclaimMessage(preempteeTask, preemptorJob)

		var queueDetails string
		if reclaimeeQueue.ParentQueue == reclaimerQueue.ParentQueue {
			queueDetails = getReclaimMessageQueuesDetails(ssn, preempteeTask, preemptorJob,
				reclaimerQueue, reclaimeeQueue)
		} else {
			queueDetails = getReclaimMessageQueuesDetails(ssn, preempteeTask, preemptorJob,
				reclaimerParentQueue, reclaimeeParentQueue)
		}

		msg += "\n" + queueDetails
		return msg
	}

	log.InfraLogger.Errorf("Unexpected action type: <%v>, task: <%v/%v>", actionType, preempteeTask.Namespace,
		preempteeTask.Name)
	return ""
}

func getReclaimMessageQueuesDetails(ssn *framework.Session, reclaimeeTask *pod_info.PodInfo,
	reclaimerJob *podgroup_info.PodGroupInfo, reclaimerQueue *queue_info.QueueInfo,
	reclaimeeQueue *queue_info.QueueInfo,
) string {
	reclaimeeAllocatedResource := ssn.QueueAllocatedResources(reclaimeeQueue)
	reclaimeeDeservedResource := ssn.QueueDeservedResources(reclaimeeQueue)
	reclaimeeFairShare := ssn.QueueFairShare(reclaimeeQueue)

	reclaimerAllocatedResource := ssn.QueueAllocatedResources(reclaimerQueue)
	reclaimerDeservedResource := ssn.QueueDeservedResources(reclaimerQueue)
	reclaimerFairShare := ssn.QueueFairShare(reclaimerQueue)

	if reclaimeeAllocatedResource == nil || reclaimeeDeservedResource == nil || reclaimeeFairShare == nil ||
		reclaimerAllocatedResource == nil || reclaimerDeservedResource == nil || reclaimerFairShare == nil {
		log.InfraLogger.Errorf(
			"Failed to get reclaimer or reclaimee queue details - task: "+
				"<%s/%s> was reclaimed by job: <%s/%s> reclaimeeAllocatedResource: %v,"+
				" reclaimeeDeservedResource: %v, reclaimeeFairShare: %v, reclaimerAllocatedResource: %v, "+
				"reclaimerDeservedResource: %v reclaimerFairShare: %v",
			reclaimeeTask.Namespace, reclaimeeTask.Name, reclaimerJob.Namespace, reclaimerJob.Name,
			reclaimeeAllocatedResource, reclaimeeDeservedResource, reclaimeeFairShare,
			reclaimerAllocatedResource, reclaimerDeservedResource, reclaimerFairShare)
		return ""
	}

	return api.GetReclaimQueueDetailsMessage(reclaimeeQueue.Name, reclaimeeAllocatedResource,
		reclaimeeDeservedResource, reclaimeeFairShare, reclaimeeQueue.Priority) + "\n" +
		api.GetReclaimQueueDetailsMessage(reclaimerQueue.Name, reclaimerAllocatedResource,
			reclaimerDeservedResource, reclaimerFairShare, reclaimerQueue.Priority)
}

func GetAllPendingJobs(ssn *framework.Session) map[common_info.PodGroupID]*podgroup_info.PodGroupInfo {
	pendingJobs := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
	for _, job := range ssn.PodGroupInfos {
		if len(job.PodStatusIndex[pod_status.Pending]) > 0 {
			pendingJobs[job.UID] = job
		}
	}
	return pendingJobs
}

func IsEnoughGPUsAllocatableForJob(job *podgroup_info.PodGroupInfo, ssn *framework.Session, isRealAllocation bool) bool {
	sumOfAllAllocatableGPUs, sumOfAllAllocatableGPUsMemory := getSumOfAvailableGPUs(ssn)
	requestedGPUs, requestedGpuMemory := podgroup_info.GetTasksToAllocateRequestedGPUs(job, ssn.TaskOrderFn, isRealAllocation)
	resReq := podgroup_info.GetTasksToAllocateInitResource(job, ssn.TaskOrderFn, isRealAllocation, ssn.MinNodeGPUMemory)
	log.InfraLogger.V(7).Infof(
		"Task: <%v/%v> resources requires: <%v>, sumOfAllAllocatableGPUs: <%v, %v mb>",
		job.Namespace, job.Name, resReq, sumOfAllAllocatableGPUs, sumOfAllAllocatableGPUsMemory)
	return sumOfAllAllocatableGPUs >= requestedGPUs && sumOfAllAllocatableGPUsMemory >= requestedGpuMemory
}

func getSumOfAvailableGPUs(ssn *framework.Session) (float64, int64) {
	sumOfAllAllocatableGPUs := float64(0)
	sumOfAllAllocatableGPUsMemory := int64(0)
	for _, node := range ssn.Nodes {
		if !scheduler_util.ValidateIsNodeReady(node.Node) {
			continue
		}

		idleGPUs, idleGPUsMemory := node.GetSumOfIdleGPUs()
		sumOfAllAllocatableGPUs += idleGPUs
		sumOfAllAllocatableGPUsMemory += idleGPUsMemory

		releasingGPUs, releasingGPUsMemory := node.GetSumOfReleasingGPUs()
		sumOfAllAllocatableGPUs += releasingGPUs
		sumOfAllAllocatableGPUsMemory += releasingGPUsMemory
	}

	return sumOfAllAllocatableGPUs, sumOfAllAllocatableGPUsMemory
}
