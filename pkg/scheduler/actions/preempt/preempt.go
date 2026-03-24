// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

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
)

type preemptAction struct {
}

func New() *preemptAction {
	return &preemptAction{}
}

func (alloc *preemptAction) Name() framework.ActionType {
	return framework.Preempt
}

func (alloc *preemptAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter Preempt ...")
	defer log.InfraLogger.V(2).Infof("Leaving Preempt ...")

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: ssn.GetJobsDepth(framework.Preempt),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}

	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()

		smallestFailedJobs, found := smallestFailedJobsByQueue[job.Queue]
		if !found {
			smallestFailedJobsByQueue[job.Queue] = common.NewMinimalJobRepresentatives()
			smallestFailedJobs = smallestFailedJobsByQueue[job.Queue]
		}
		if ssn.UseSchedulingSignatures() {
			easier, otherJob := smallestFailedJobs.IsEasierToSchedule(job)
			if !easier {
				log.InfraLogger.V(3).Infof(
					"Skipping preemption for job: <%v/%v> - is not easier to preempt for than: <%v/%v>",
					job.Namespace, job.Name, otherJob.Namespace, otherJob.Name)
				continue
			}
		}

		metrics.IncPodgroupsConsideredByAction()
		succeeded, statement, preemptedTasksNames := attemptToPreemptForPreemptor(ssn, job)
		if succeeded {
			metrics.RegisterPreemptionAttempts()
			metrics.IncPodgroupScheduledByAction()
			log.InfraLogger.V(3).Infof(
				"Successfully preempted for job <%s/%s>, preempted tasks: <%v>",
				job.Namespace, job.Name, preemptedTasksNames)
			if err := statement.Commit(); err != nil {
				log.InfraLogger.Errorf("Failed to commit preemption statement: %v", err)
			}
		} else {
			log.InfraLogger.V(3).Infof("Didn't find a preemption strategy for job <%s/%s>",
				job.Namespace, job.Name)
			smallestFailedJobs.UpdateRepresentative(job)
		}
	}
}

func attemptToPreemptForPreemptor(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo,
) (bool, *framework.Statement, []string) {
	resReq := podgroup_info.GetTasksToAllocateInitResource(preemptor, ssn.TaskOrderFn, false, ssn.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof(
		"Attempting to preempt for job: <%v/%v>, priority: <%v>, queue: <%v>, resources: <%v>",
		preemptor.Namespace, preemptor.Name, preemptor.Priority, preemptor.Queue, resReq)

	preemptorTasks := podgroup_info.GetTasksToAllocate(preemptor, ssn.TaskOrderFn, false)
	if result := ssn.IsNonPreemptibleJobOverQueueQuotaFn(preemptor, preemptorTasks); !result.IsSchedulable {
		log.InfraLogger.V(3).Infof("Job <%v/%v> would have placed the queue resources over quota",
			preemptor.Namespace, preemptor.Name)
		return false, nil, nil
	}

	feasibleNodes := common.FeasibleNodesForJob(maps.Values(ssn.Nodes), preemptor)
	solver := solvers.NewJobsSolver(
		feasibleNodes,
		ssn.PreemptScenarioValidator,
		getOrderedVictimsQueue(ssn, preemptor),
		framework.Preempt,
	)
	return solver.Solve(ssn, preemptor)
}

func buildFilterFuncForPreempt(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) func(*podgroup_info.PodGroupInfo) bool {
	return func(job *podgroup_info.PodGroupInfo) bool {
		if !job.IsPreemptibleJob(ssn.IsInferencePreemptible()) {
			return false
		}

		if job.Priority >= preemptor.Priority {
			return false
		}

		if job.Queue != preemptor.Queue {
			return false
		}

		// Preempt other jobs
		if preemptor.UID == job.UID {
			return false
		}

		if job.GetActiveAllocatedTasksCount() == 0 {
			return false
		}

		if !ssn.PreemptVictimFilter(preemptor, job) {
			return false
		}

		return true
	}
}

func getOrderedVictimsQueue(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) solvers.GenerateVictimsQueue {
	return func() *utils.JobsOrderByQueues {
		filter := buildFilterFuncForPreempt(ssn, preemptor)
		victimsQueue := utils.GetVictimsQueue(ssn, filter)
		return victimsQueue
	}
}
