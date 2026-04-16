// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

type JobsOrderInitOptions struct {
	FilterUnready            bool
	FilterNonPending         bool
	FilterNonPreemptible     bool
	FilterNonActiveAllocated bool
	VictimQueue              bool
	MaxJobsQueueDepth        int
}

func (jobsOrder *JobsOrderByQueues) InitializeWithJobs(
	jobsToOrder map[common_info.PodGroupID]*podgroup_info.PodGroupInfo) {
	for _, job := range jobsToOrder {
		if jobsOrder.jobsOrderInitOptions.FilterUnready && !job.IsReadyForScheduling() {
			continue
		}

		if jobsOrder.jobsOrderInitOptions.FilterNonPending && len(job.PodStatusIndex[pod_status.Pending]) == 0 {
			continue
		}

		if jobsOrder.jobsOrderInitOptions.FilterNonPreemptible && !job.IsPreemptibleJob(jobsOrder.ssn.IsInferencePreemptible()) {
			continue
		}

		isJobActive := false
		for _, task := range job.PodInfos {
			if pod_status.IsActiveAllocatedStatus(task.Status) {
				isJobActive = true
				break
			}
		}
		if jobsOrder.jobsOrderInitOptions.FilterNonActiveAllocated && !isJobActive {
			continue
		}

		jobsOrder.addJobToQueue(job, jobsOrder.jobsOrderInitOptions.VictimQueue)
	}

	jobsOrder.buildActiveJobOrderPriorityQueues(jobsOrder.jobsOrderInitOptions.VictimQueue)
}
