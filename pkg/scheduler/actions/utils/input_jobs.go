// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"time"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type JobsOrderInitOptions struct {
	FilterUnready            bool
	FilterNonPending         bool
	FilterNonPreemptible     bool
	FilterNonActiveAllocated bool
	// FilterWithinPreemptionDelay skips jobs that may not yet trigger eviction
	// of other workloads. Set only by eviction-triggering actions.
	FilterWithinPreemptionDelay bool
	VictimQueue                 bool
	MaxJobsQueueDepth           int
}

func (jobsOrder *JobsOrderByQueues) InitializeWithJobs(
	jobsToOrder map[common_info.PodGroupID]*podgroup_info.PodGroupInfo) {
	now := time.Now()
	for _, job := range jobsToOrder {
		if jobsOrder.options.FilterUnready && !job.IsReadyForScheduling() {
			continue
		}

		if jobsOrder.options.FilterNonPending && len(job.PodStatusIndex[pod_status.Pending]) == 0 {
			continue
		}

		if jobsOrder.options.FilterNonPreemptible && !job.IsPreemptibleJob() {
			continue
		}

		isJobActive := false
		for _, task := range job.GetAllPodsMap() {
			if pod_status.IsActiveAllocatedStatus(task.Status) {
				isJobActive = true
				break
			}
		}
		if jobsOrder.options.FilterNonActiveAllocated && !isJobActive {
			continue
		}

		// Skip jobs whose queue doesn't exist
		queues := jobsOrder.ssn.ClusterInfo.Queues
		if _, found := queues[job.Queue]; !found {
			continue
		}

		// Skip jobs whose queue's parent queue doesn't exist (unless it's a root queue)
		parentQueue := queues[job.Queue].ParentQueue
		if parentQueue != "" {
			if _, found := queues[parentQueue]; !found {
				continue
			}
		}

		// Skip jobs whose queue is not a leaf queue
		if !jobsOrder.ssn.ClusterInfo.Queues[job.Queue].IsLeafQueue() {
			continue
		}

		if jobsOrder.options.FilterWithinPreemptionDelay && job.IsWithinPreemptionDelay(now) {
			recordPreemptionDelayFitError(job)
			log.InfraLogger.V(3).Infof("Job <%s> is within its preemption delay window, skipping as eviction trigger",
				job.NamespacedName)
			continue
		}

		jobsOrder.PushJob(job)
	}
}

func recordPreemptionDelayFitError(job *podgroup_info.PodGroupInfo) {
	for _, fitErr := range job.JobFitErrors {
		if fitErr.Reason() == enginev2alpha2.PreemptionDelayNotElapsed {
			return
		}
	}
	job.AddSimpleJobFitError(enginev2alpha2.PreemptionDelayNotElapsed,
		fmt.Sprintf("Workload is within its preemption delay window (%s) and may not trigger evictions before %s",
			job.PodGroup.Spec.PreemptionDelay.Duration, job.PreemptionDelayEnd().UTC().Format(time.RFC3339)))
}
