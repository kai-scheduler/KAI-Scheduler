// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"golang.org/x/exp/maps"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/metrics"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/scheduler_util"
)

type reclaimAction struct {
}

func New() *reclaimAction {
	return &reclaimAction{}
}

func (ra *reclaimAction) Name() framework.ActionType {
	return framework.Reclaim
}

func (ra *reclaimAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter Reclaim ...")
	defer log.InfraLogger.V(2).Infof("Leaving Reclaim ...")

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: ssn.GetJobsDepth(framework.Reclaim),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}

	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()
		if !ssn.CanReclaimResources(job) {
			continue
		}

		smallestFailedJobs, found := smallestFailedJobsByQueue[job.Queue]
		if !found {
			smallestFailedJobsByQueue[job.Queue] = common.NewMinimalJobRepresentatives()
			smallestFailedJobs = smallestFailedJobsByQueue[job.Queue]
		}
		if ssn.UseSchedulingSignatures() {
			easier, otherJob := smallestFailedJobs.IsEasierToSchedule(job)
			if !easier {
				log.InfraLogger.V(3).Infof(
					"Skipping reclaim for job: <%v/%v> - is not easier to reclaim for than: <%v/%v>",
					job.Namespace, job.Name, otherJob.Namespace, otherJob.Name)
				continue
			}
		}
		metrics.IncPodgroupsConsideredByAction()
		succeeded, statement, reclaimeeTasksNames := ra.attemptToReclaimForSpecificJob(ssn, job)
		if succeeded {
			metrics.IncPodgroupScheduledByAction()
			log.InfraLogger.V(3).Infof(
				"Reclaimed resources for job <%s/%s>, evicting reclaimee tasks: <%v>.",
				job.Namespace, job.Name, reclaimeeTasksNames,
			)
			if err := statement.Commit(); err != nil {
				log.InfraLogger.Errorf("Failed to commit reclaim statement: %v", err)
			}
		} else {
			log.InfraLogger.V(3).Infof("Didn't find a reclaim strategy for job <%s/%s>",
				job.Namespace, job.Name)
			smallestFailedJobs.UpdateRepresentative(job)
		}
	}
}

func (ra *reclaimAction) attemptToReclaimForSpecificJob(
	ssn *framework.Session, reclaimer *podgroup_info.PodGroupInfo,
) (bool, *framework.Statement, []string) {
	queue := ssn.Queues[reclaimer.Queue]
	resReq := podgroup_info.GetTasksToAllocateInitResource(reclaimer, ssn.TaskOrderFn, false, ssn.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof("Attempting to reclaim for job: <%v/%v> of queue <%v>, resources: <%v>",
		reclaimer.Namespace, reclaimer.Name, queue.Name, resReq)

	ssn.OnJobSolutionStart()

	feasibleNodes := common.FeasibleNodesForJob(maps.Values(ssn.Nodes), reclaimer)
	solver := solvers.NewJobsSolver(
		feasibleNodes,
		ssn.ReclaimScenarioValidatorFn,
		getOrderedVictimsQueue(ssn, reclaimer),
		framework.Reclaim)
	return solver.Solve(ssn, reclaimer)
}

func getOrderedVictimsQueue(ssn *framework.Session, reclaimer *podgroup_info.PodGroupInfo) solvers.GenerateVictimsQueue {
	return func() *utils.JobsOrderByQueues {
		jobsOrderedByQueue := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
			FilterNonPreemptible:     true,
			FilterNonActiveAllocated: true,
			VictimQueue:              true,
			MaxJobsQueueDepth:        scheduler_util.QueueCapacityInfinite,
		})
		jobs := map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{}
		for _, job := range ssn.PodGroupInfos {
			if job.Queue == reclaimer.Queue {
				continue
			}
			if !ssn.ReclaimVictimFilter(reclaimer, job) {
				continue
			}
			jobs[job.UID] = job
		}

		jobsOrderedByQueue.InitializeWithJobs(jobs)
		return &jobsOrderedByQueue
	}
}
