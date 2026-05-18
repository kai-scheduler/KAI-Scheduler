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
	"fmt"
	"sort"
	"strings"

	"golang.org/x/exp/maps"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
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

	jobsOrderByQueues := utils.NewJobsOrderByQueues(ssn, utils.JobsOrderInitOptions{
		FilterNonPending:  true,
		FilterUnready:     true,
		MaxJobsQueueDepth: ssn.GetJobsDepth(framework.Preempt),
	})
	jobsOrderByQueues.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)

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
				job.AddJobFitError(common_info.NewLazyJobFitError(
					enginev2alpha2.PreemptNoSolutionFound,
					"Preempt: skipped after considering equivalent job %s/%s",
					otherJob.Namespace, otherJob.Name,
				))
				continue
			}
		}
		tasks := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
		if task, failure := common.VictimInvariantPrePredicateFailureForTasks(ssn, tasks); failure != nil {
			common.RecordVictimInvariantPrePredicateFailure(job, task, failure)
			continue
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
	resReq := podgroup_info.GetTasksToAllocateInitResourceVector(preemptor, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false, ssn.ClusterInfo.MinNodeGPUMemory)
	log.InfraLogger.V(3).Infof(
		"Attempting to preempt for job: <%v/%v>, priority: <%v>, queue: <%v>, resources: <%v>",
		preemptor.Namespace, preemptor.Name, preemptor.Priority, preemptor.Queue, resReq)

	preemptorTasks := podgroup_info.GetTasksToAllocate(preemptor, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	if result := ssn.IsNonPreemptibleJobOverQueueQuotaFn(preemptor, preemptorTasks); !result.IsSchedulable {
		log.InfraLogger.V(3).Infof("Job <%v/%v> would have placed the queue resources over quota",
			preemptor.Namespace, preemptor.Name)
		preemptor.AddJobFitError(common_info.NewLazyJobFitError(
			enginev2alpha2.PreemptOverQueueQuota,
			"Preempt: %s", result.Message,
		))
		return false, nil, nil
	}

	feasibleNodes := common.FeasibleNodesForJob(maps.Values(ssn.ClusterInfo.Nodes), preemptor)
	solver := solvers.NewJobsSolver(
		feasibleNodes,
		ssn.PreemptScenarioValidator,
		getOrderedVictimsQueue(ssn, preemptor),
		framework.Preempt,
	)
	solved, stmt, victimNames, validatorReject := solver.Solve(ssn, preemptor)
	if !solved {
		if validatorReject != nil {
			preemptor.AddJobFitError(common_info.NewLazyJobFitErrorFromFilterResult(*validatorReject))
		} else {
			preemptor.AddJobFitError(common_info.NewLazyJobFitError(
				enginev2alpha2.PreemptNoSolutionFound,
				"Preempt: no feasible preemption scenario found for job %s/%s",
				preemptor.Namespace, preemptor.Name,
			))
		}
	}
	return solved, stmt, victimNames
}

func buildFilterFuncForPreempt(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) func(*podgroup_info.PodGroupInfo) common_info.FilterResult {
	return func(job *podgroup_info.PodGroupInfo) common_info.FilterResult {
		if preemptor.UID == job.UID {
			// silently skip self
			return common_info.Pass()
		}
		if !job.IsPreemptibleJob() {
			return common_info.Reject(
				enginev2alpha2.PreemptNoEligibleVictims,
				"victim %s/%s is not preemptible",
				job.Namespace, job.Name,
			)
		}
		if job.Priority >= preemptor.Priority {
			return common_info.Reject(
				enginev2alpha2.PreemptNoEligibleVictims,
				"victim %s/%s priority %d >= preemptor priority %d",
				job.Namespace, job.Name, job.Priority, preemptor.Priority,
			)
		}
		if job.Queue != preemptor.Queue {
			return common_info.Reject(
				enginev2alpha2.PreemptNoEligibleVictims,
				"victim %s/%s is in different queue",
				job.Namespace, job.Name,
			)
		}
		if job.GetActiveAllocatedTasksCount() == 0 {
			return common_info.Reject(
				enginev2alpha2.PreemptNoEligibleVictims,
				"victim %s/%s has no active allocated tasks",
				job.Namespace, job.Name,
			)
		}
		return ssn.PreemptVictimFilter(preemptor, job)
	}
}

func getOrderedVictimsQueue(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) solvers.GenerateVictimsQueue {
	return func() *utils.JobsOrderByQueues {
		filter := buildFilterFuncForPreempt(ssn, preemptor)
		boolFilter, recordRejection := wrapFilterWithRejectionAggregator(filter)
		victimsQueue := utils.GetVictimsQueue(ssn, boolFilter)
		recordRejection(preemptor)
		return victimsQueue
	}
}

// wrapFilterWithRejectionAggregator adapts a FilterResult-returning filter to
// the bool-returning shape utils.GetVictimsQueue expects, while collecting per-
// reason rejection counts. The returned recorder publishes a single
// PreemptNoEligibleVictims error on the preemptor when it processed at least
// one rejected candidate and accepted none.
func wrapFilterWithRejectionAggregator(
	filter func(*podgroup_info.PodGroupInfo) common_info.FilterResult,
) (
	func(*podgroup_info.PodGroupInfo) bool,
	func(*podgroup_info.PodGroupInfo),
) {
	rejectionCounts := map[enginev2alpha2.UnschedulableReason]int{}
	totalRejected := 0
	totalAccepted := 0
	boolFilter := func(job *podgroup_info.PodGroupInfo) bool {
		result := filter(job)
		if !result.Passed {
			rejectionCounts[result.ReasonCode]++
			totalRejected++
			return false
		}
		totalAccepted++
		return true
	}
	recorder := func(preemptor *podgroup_info.PodGroupInfo) {
		if totalAccepted > 0 || totalRejected == 0 {
			return
		}
		preemptor.AddJobFitError(common_info.NewLazyJobFitError(
			enginev2alpha2.PreemptNoEligibleVictims,
			"Preempt: all %d in-queue candidates filtered (%s)",
			totalRejected, formatPreemptRejectionCounts(rejectionCounts),
		))
	}
	return boolFilter, recorder
}

func formatPreemptRejectionCounts(counts map[enginev2alpha2.UnschedulableReason]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d by %s", counts[enginev2alpha2.UnschedulableReason(k)], k))
	}
	return strings.Join(parts, ", ")
}
