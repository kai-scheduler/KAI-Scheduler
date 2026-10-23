/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

import (
	"golang.org/x/exp/maps"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/deferredresize"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
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

	actionBudget, err := solvers.NewActionSearchBudget(ssn, framework.Preempt)
	if err != nil {
		log.InfraLogger.Errorf("Invalid scenario search budget for preempt: %v", err)
		return
	}

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:            true,
		FilterUnready:               true,
		FilterWithinPreemptionDelay: true,
		MaxJobsQueueDepth:           ssn.GetJobsDepth(framework.Preempt),
		NonPendingJobFilter:         deferredresize.NonPendingJobFilter(ssn),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

	log.InfraLogger.V(2).Infof("There are <%d> PodGroupInfos and <%d> Queues in total for scheduling",
		jobsOrderByQueues.Len(), ssn.CountLeafQueues())

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}

	for !jobsOrderByQueues.IsEmpty() {
		job := jobsOrderByQueues.PopNextJob()

		if shouldStop := preemptForPendingTasks(ssn, job, actionBudget, smallestFailedJobsByQueue); shouldStop {
			return
		}

		// Pending pods get the first pick of victims; the resize is tried afterwards either way.
		if shouldStop := deferredresize.AttemptForJob(ssn, job, framework.Preempt,
			solveDeferredResize(ssn, actionBudget), shouldStopActionForSearchResult); shouldStop {
			return
		}
	}
}

func preemptForPendingTasks(
	ssn *framework.Session, job *podgroup_info.PodGroupInfo, actionBudget *solvers.ActionSearchBudget,
	smallestFailedJobsByQueue map[common_info.QueueID]*common.MinimalJobRepresentatives,
) (shouldStop bool) {
	if job.GetNumPendingTasks() == 0 {
		return false
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
				"Skipping preemption for job: <%v/%v> - is not easier to preempt for than: <%v/%v>",
				job.Namespace, job.Name, otherJob.Namespace, otherJob.Name)
			return false
		}
	}
	tasks := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation)
	if task, failure := common.VictimInvariantPrePredicateFailureForTasks(ssn, tasks); failure != nil {
		common.RecordVictimInvariantPrePredicateFailure(job, task, failure)
		return false
	}

	metrics.IncPodgroupsConsideredByAction()
	succeeded, statement, preemptedTasksNames, searchResult := attemptToPreemptForPreemptor(ssn, job, actionBudget)
	if succeeded {
		metrics.RegisterPreemptionAttempts()
		metrics.IncPodgroupScheduledByAction()
		log.InfraLogger.V(3).Infof(
			"Successfully preempted for job <%s/%s>, preempted tasks: <%v>",
			job.Namespace, job.Name, preemptedTasksNames)
		if err := statement.Commit(); err != nil {
			log.InfraLogger.Errorf("Failed to commit preemption statement: %v", err)
		}
	} else if shouldStopActionForSearchResult(searchResult) {
		return true
	} else {
		log.InfraLogger.V(3).Infof("Didn't find a preemption strategy for job <%s/%s>",
			job.Namespace, job.Name)
		smallestFailedJobs.UpdateRepresentative(job)
	}
	return false
}

func solveDeferredResize(ssn *framework.Session, actionBudget *solvers.ActionSearchBudget) deferredresize.Solve {
	return func(
		job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo, task *pod_info.PodInfo,
	) (*framework.Statement, *solvers.SearchResult) {
		tasks := []*pod_info.PodInfo{task}
		if result := ssn.IsNonPreemptibleJobOverQueueQuotaFn(job, tasks); !result.IsSchedulable {
			log.InfraLogger.V(3).Infof("Deferred resize of job <%v/%v> would place the queue over quota",
				job.Namespace, job.Name)
			return nil, nil
		}
		_, statement, _, result := solvers.NewJobsSolver(
			[]*node_info.NodeInfo{node},
			ssn.PreemptScenarioValidator,
			getOrderedVictimsQueueOnNode(ssn, job, node.Name),
			framework.Preempt,
			actionBudget,
		).PinFeasibleNodes().SolveTasksWithResult(ssn, job, tasks)
		return statement, result
	}
}

func attemptToPreemptForPreemptor(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo, actionBudget *solvers.ActionSearchBudget,
) (bool, *framework.Statement, []string, *solvers.SearchResult) {
	resReq := podgroup_info.GetTasksToAllocateInitResourceVector(preemptor, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation, ssn.ClusterInfo.MinNodeGPUMemoryMiB)
	log.InfraLogger.V(3).Infof(
		"Attempting to preempt for job: <%v/%v>, priority: <%v>, queue: <%v>, resources: <%v>",
		preemptor.Namespace, preemptor.Name, preemptor.Priority, preemptor.Queue, resReq)

	preemptorTasks := podgroup_info.GetTasksToAllocate(preemptor, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation)
	if result := ssn.IsNonPreemptibleJobOverQueueQuotaFn(preemptor, preemptorTasks); !result.IsSchedulable {
		log.InfraLogger.V(3).Infof("Job <%v/%v> would have placed the queue resources over quota",
			preemptor.Namespace, preemptor.Name)
		return false, nil, nil, nil
	}

	feasibleNodes := common.FeasibleNodesForJob(maps.Values(ssn.ClusterInfo.Nodes), preemptor)
	solver := solvers.NewJobsSolver(
		feasibleNodes,
		ssn.PreemptScenarioValidator,
		getOrderedVictimsQueue(ssn, preemptor),
		framework.Preempt,
		actionBudget,
	)
	return solver.SolveWithResult(ssn, preemptor)
}

func shouldStopActionForSearchResult(result *solvers.SearchResult) bool {
	switch result.Reason() {
	case solvers.SearchResultDeadlineExhausted, solvers.SearchResultNotAttempted:
		return true
	default:
		return false
	}
}

func buildFilterFuncForPreempt(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) func(*podgroup_info.PodGroupInfo) bool {
	return func(job *podgroup_info.PodGroupInfo) bool {
		if !job.HasEvictableTasks() {
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
	return newVictimsQueueGenerator(ssn, buildFilterFuncForPreempt(ssn, preemptor))
}

func getOrderedVictimsQueueOnNode(
	ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo, nodeName string,
) solvers.GenerateVictimsQueue {
	filter := buildFilterFuncForPreempt(ssn, preemptor)
	return newVictimsQueueGenerator(ssn, func(job *podgroup_info.PodGroupInfo) bool {
		return deferredresize.HasActiveAllocatedTaskOnNode(job, nodeName) && filter(job)
	})
}

func newVictimsQueueGenerator(
	ssn *framework.Session, filter func(*podgroup_info.PodGroupInfo) bool,
) solvers.GenerateVictimsQueue {
	return utils.NewCachedVictimsQueueGenerator(
		ssn,
		func() map[common_info.PodGroupID]*podgroup_info.PodGroupInfo {
			return utils.GetVictimCandidates(ssn, filter)
		},
		utils.JobsOrderInitOptions{
			FilterNonPreemptible:     true,
			FilterNonActiveAllocated: true,
		},
	)
}
