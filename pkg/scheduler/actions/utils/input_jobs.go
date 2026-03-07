// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
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

		jobsOrder.PushJob(job)
	}
}
