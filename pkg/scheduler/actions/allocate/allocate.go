// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package allocate

import (
	"time"

	"golang.org/x/exp/maps"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
)

type allocateAction struct {
}

func New() *allocateAction {
	return &allocateAction{}
}

func (alloc *allocateAction) Name() framework.ActionType {
	return framework.Allocate
}

func (alloc *allocateAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter Allocate ...")
	defer log.InfraLogger.V(2).Infof("Leaving Allocate ...")

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: ssn.GetJobsDepth(framework.Allocate),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())
	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()
		stmt := ssn.Statement()
		alreadyAllocated := job.GetNumAllocatedTasks() > 0
		if ok, pipelined := attemptToAllocateJob(ssn, stmt, job); ok {
			metrics.IncPodgroupScheduledByAction()
			err := stmt.Commit()
			if err == nil && !pipelined && !alreadyAllocated {
				setLastStartTimestamp(job)
			}
			if err == nil && podgroup_info.HasTasksToAllocate(job, true) {
				jobsOrderByQueues.PushJob(job)
				continue
			}
		} else {
			stmt.Discard()
		}
	}
}

func attemptToAllocateJob(ssn *framework.Session, stmt *framework.Statement, job *podgroup_info.PodGroupInfo) (allocated, pipelined bool) {
	queue := ssn.Queues[job.Queue]

	resReq := podgroup_info.GetTasksToAllocateInitResource(job, ssn.TaskOrderFn, true, ssn.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof("Attempting to allocate job: <%v/%v> of queue <%v>, resources: <%v>",
		job.Namespace, job.Name, queue.Name, resReq)

	nodes := maps.Values(ssn.Nodes)
	if !common.AllocateJob(ssn, stmt, nodes, job, false) {
		log.InfraLogger.V(3).Infof("Could not allocate resources for job: <%v/%v> of queue <%v>",
			job.Namespace, job.Name, job.Queue)
		return false, false
	}
	pipelined = false
	if job.ShouldPipelineJob() {
		log.InfraLogger.V(3).Infof(
			"Some tasks were pipelined, setting all job to be pipelined for job: <%v/%v>",
			job.Namespace, job.Name)
		err := stmt.ConvertAllAllocatedToPipelined(job.UID)
		if err != nil {
			log.InfraLogger.Errorf(
				"Failed to covert tasks from allocated to pipelined for job: <%v/%v>, error: <%v>",
				job.Namespace, job.Name, err)
			return false, false
		}
		pipelined = true
	} else {
		log.InfraLogger.V(3).Infof("Succesfully allocated resources for job: <%v/%v>",
			job.Namespace, job.Name)
	}

	return true, pipelined
}

func setLastStartTimestamp(job *podgroup_info.PodGroupInfo) {
	timeNow := time.Now()
	job.LastStartTimestamp = &timeNow
}
