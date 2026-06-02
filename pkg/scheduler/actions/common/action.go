// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func EvictAllPreemptees(ssn *framework.Session, preempteeTasks []*pod_info.PodInfo,
	preemptor *podgroup_info.PodGroupInfo, stmt *framework.Statement,
	actionType framework.ActionType) error {

	for _, task := range preempteeTasks {
		// Materialize the eviction message lazily at commit time; on the hot
		// probe-and-discard path the statement is rolled back and the builder
		// is never invoked.
		messageBuilder := func() string {
			return utils.GetMessageOfEviction(ssn, actionType, task, preemptor)
		}
		err := stmt.Evict(task, messageBuilder, eviction_info.EvictionMetadata{
			Action:           string(actionType),
			EvictionGangSize: len(preempteeTasks),
			Preemptor:        &types.NamespacedName{Namespace: preemptor.Namespace, Name: preemptor.Name},
		})
		if err != nil {
			log.InfraLogger.Errorf("Failed to preempt task <%s/%s> for PodInfos <%s/%s>: %v",
				task.Namespace, task.Name, preemptor.Namespace, preemptor.Name, err)
			return fmt.Errorf("failed to evict all preemptees. error: %s", err)
		}
	}

	return nil
}

func GetJobsToAllocate(ssn *framework.Session, preempteeTasks []*pod_info.PodInfo,
	preemptor *podgroup_info.PodGroupInfo) *utils.JobsOrderByQueues {
	allJobsToAllocate := utils.GetAllPendingJobs(ssn)
	for _, task := range preempteeTasks {
		preempteeJob := ssn.ClusterInfo.PodGroupInfos[task.Job]
		allJobsToAllocate[preempteeJob.UID] = preempteeJob
	}
	// add preemptor to allJobsToAllocate if it's not there
	allJobsToAllocate[preemptor.UID] = preemptor
	jobsToAllocateQueue := utils.NewJobsOrderByQueues(
		ssn, utils.JobsOrderInitOptions{MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite})
	jobsToAllocateQueue.InitializeWithJobs(allJobsToAllocate)
	return &jobsToAllocateQueue
}

func TryToVirtuallyAllocatePreemptorAndGetVictims(
	ssn *framework.Session, stmt *framework.Statement,
	nodes []*node_info.NodeInfo,
	preemptor *podgroup_info.PodGroupInfo,
	jobsToAllocate *utils.JobsOrderByQueues,
	preempteeTasks []*pod_info.PodInfo,
) (bool, []*pod_info.PodInfo) {
	preemptorAllocated := false
	var newVictims []*pod_info.PodInfo

	potentialVictimsMap := make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo)
	for _, task := range preempteeTasks {
		job := ssn.ClusterInfo.PodGroupInfos[task.Job]
		potentialVictimsMap[job.UID] = job
	}

	for !jobsToAllocate.IsEmpty() {
		jobToAllocate := jobsToAllocate.PopNextJob()
		if _, exits := potentialVictimsMap[jobToAllocate.UID]; !exits && jobToAllocate.UID != preemptor.UID {
			continue
		}

		resReq := podgroup_info.GetTasksToAllocateInitResourceVector(
			jobToAllocate, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false, ssn.ClusterInfo.MinNodeGPUMemory)
		log.InfraLogger.V(6).Infof("Trying to pipeline job: <%s/%s>. resources required: %v",
			jobToAllocate.Namespace, jobToAllocate.Name, resReq)

		if jobToAllocate.UID != preemptor.UID {
			if !AllocateJob(ssn, stmt, nodes, jobToAllocate, true) {
				tasksToAllocate := podgroup_info.GetTasksToAllocate(jobToAllocate, ssn.SubGroupOrderFn,
					ssn.TaskOrderFn, false)
				newVictims = append(newVictims, tasksToAllocate...)
			}
			continue
		}

		success := AllocateJob(ssn, stmt, nodes, jobToAllocate, true)
		if !success {
			return false, []*pod_info.PodInfo{}
		}
		preemptorAllocated = true
	}

	if preemptorAllocated {
		return true, newVictims
	}

	return false, []*pod_info.PodInfo{}
}
