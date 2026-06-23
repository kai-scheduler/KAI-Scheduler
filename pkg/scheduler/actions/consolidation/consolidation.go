// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation

import (
	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

const noConsolidationPreempteesRestrcition = -1

type consolidationAction struct{}

func New() *consolidationAction {
	return &consolidationAction{}
}

func (alloc *consolidationAction) Name() framework.ActionType {
	return framework.Consolidation
}

func (alloc *consolidationAction) Execute(ssn *framework.Session) {
	log.InfraLogger.V(2).Infof("Enter Consolidation ...")
	defer log.InfraLogger.V(2).Infof("Leaving Consolidation ...")

	actionBudget, err := solvers.NewActionSearchBudget(ssn, framework.Consolidation)
	if err != nil {
		log.InfraLogger.Errorf("Invalid scenario search budget for consolidation: %v", err)
		return
	}

	if ssn.GetMaxNumberConsolidationPreemptees() == 0 {
		log.InfraLogger.V(4).Infof("Consolidation is disabled, skipping")
		return
	}

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:     true,
		FilterUnready:        true,
		FilterNonPreemptible: true,
		MaxJobsQueueDepth:    ssn.GetJobsDepth(framework.Consolidation),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())

	smallestFailedJobs := common.NewMinimalJobRepresentatives()

	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()

		if ssn.UseSchedulingSignatures() {
			easier, otherJob := smallestFailedJobs.IsEasierToSchedule(job)
			if !easier {
				log.InfraLogger.V(3).Infof(
					"Skipping consolidation for job: <%v/%v> - is not easier to consolidate for than: <%v/%v>",
					job.Namespace, job.Name, otherJob.Namespace, otherJob.Name)
				continue
			}
		}
		tasks := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
		if task, failure := common.VictimInvariantPrePredicateFailureForTasks(ssn, tasks); failure != nil {
			common.RecordVictimInvariantPrePredicateFailure(job, task, failure)
			continue
		}

		metrics.IncPodgroupsConsideredByAction()
		if succeeded, stmt, searchResult := attemptToConsolidateForPreemptor(ssn, job, actionBudget); succeeded {
			metrics.IncPodgroupScheduledByAction()
			err := stmt.Commit()
			if err != nil {
				log.InfraLogger.Errorf("Failed to commit consolidation statement: %v", err)
			}
		} else if shouldStopActionForSearchResult(searchResult) {
			return
		} else {
			smallestFailedJobs.UpdateRepresentative(job)
		}
	}
}

func attemptToConsolidateForPreemptor(
	ssn *framework.Session, job *podgroup_info.PodGroupInfo, actionBudget *solvers.ActionSearchBudget,
) (bool, *framework.Statement, *solvers.SearchResult) {
	resReq := podgroup_info.GetTasksToAllocateInitResourceVector(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false, ssn.ClusterInfo.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof(
		"Attempting to consolidate running jobs in order to make room for job: <%s/%s>, resources: <%v>",
		job.Namespace, job.Name, resReq)
	if !utils.IsEnoughGPUsAllocatableForJob(job, ssn, false) {
		log.InfraLogger.V(3).Infof(
			"Can't consolidate for job: <%v/%v>, not enough allocatable GPUs in the cluster",
			job.Namespace, job.Name)
		return false, nil, nil
	}
	success, stmt, searchResult := attemptToConsolidatePreemptor(ssn, job, actionBudget)
	return success, stmt, searchResult
}

func attemptToConsolidatePreemptor(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo, actionBudget *solvers.ActionSearchBudget,
) (bool, *framework.Statement, *solvers.SearchResult) {
	feasibleNodes := common.FeasibleNodesForJob(maps.Values(ssn.ClusterInfo.Nodes), preemptor)
	solver := solvers.NewJobsSolver(
		feasibleNodes,
		allPodsReallocated,
		func() *utils.JobsOrderByQueues { return buildConsolidationVictimsQueue(ssn, preemptor) },
		framework.Consolidation,
		actionBudget)

	isScenarioFeasible, stmt, victimsTasksNames, searchResult := solver.SolveWithResult(ssn, preemptor)
	if isScenarioFeasible {
		log.InfraLogger.V(3).Infof(
			"Sucesfully consolidated for job: <%s/%s>, and about to reallocate victims: <%v>",
			preemptor.Namespace, preemptor.Name, victimsTasksNames)
		return true, stmt, searchResult
	}

	if shouldStopActionForSearchResult(searchResult) {
		return false, nil, searchResult
	}

	log.InfraLogger.V(3).Infof("Didn't find a consolidation strategy for job: <%v/%v>",
		preemptor.Namespace, preemptor.Name)
	return false, nil, searchResult
}

func shouldStopActionForSearchResult(result *solvers.SearchResult) bool {
	switch result.Reason() {
	case solvers.SearchResultDeadlineExhausted, solvers.SearchResultNotAttempted:
		return true
	default:
		return false
	}
}

func allPodsReallocated(scenario api.ScenarioInfo) bool {
	for _, victim := range scenario.GetVictims() {
		for _, task := range victim.Tasks {
			if task.Status == pod_status.Releasing {
				return false
			}
		}
	}
	return true
}

func buildConsolidationVictimsQueue(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) *utils.JobsOrderByQueues {
	filter := buildPreemptibleFilterFunc(preemptor, ssn.GetMaxNumberConsolidationPreemptees())
	return utils.GetVictimsQueue(ssn, filter)
}

func buildPreemptibleFilterFunc(preemptor *podgroup_info.PodGroupInfo, maxPreempteesToTest int) func(*podgroup_info.PodGroupInfo) bool {
	preempteeJobsCounter := 0

	return func(job *podgroup_info.PodGroupInfo) bool {
		if !job.IsPreemptibleJob() {
			return false
		}

		if preemptor.UID == job.UID {
			return false
		}

		if maxPreempteesToTest != noConsolidationPreempteesRestrcition && preempteeJobsCounter > maxPreempteesToTest {
			return false
		}

		if job.GetActiveAllocatedTasksCount() == 0 {
			return false
		}

		preempteeJobsCounter += 1
		return true
	}
}
