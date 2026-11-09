// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func TestInitializeWithJobsNonPendingJobFilter(t *testing.T) {
	jobWithStatus := func(name string, status pod_status.PodStatus, queue common_info.QueueID) *podgroup_info.PodGroupInfo {
		job := podGroupForJobOrderTest(name, common_info.PodGroupID(name), 100)
		job.Queue = queue
		job.PodStatusIndex = map[pod_status.PodStatus]pod_info.PodsMap{status: {testPod: {UID: testPod}}}
		return job
	}
	popAll := func(filter func(*podgroup_info.PodGroupInfo) bool) []string {
		ssn := newPrioritySession(t)
		ssn.ClusterInfo.Queues = map[common_info.QueueID]*queue_info.QueueInfo{
			testQueue:       {UID: testQueue, ParentQueue: testParentQueue},
			testParentQueue: {UID: testParentQueue, ChildQueues: []common_info.QueueID{testQueue}},
		}
		ssn.ClusterInfo.PodGroupInfos = map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
			"pending":          jobWithStatus("pending", pod_status.Pending, testQueue),
			"running":          jobWithStatus("running", pod_status.Running, testQueue),
			"running-no-queue": jobWithStatus("running-no-queue", pod_status.Running, "missing-queue"),
		}
		jobsOrder := NewJobsOrderByQueues(ssn, JobsOrderInitOptions{
			FilterNonPending:    true,
			MaxJobsQueueDepth:   scheduler_util.QueueCapacityInfinite,
			NonPendingJobFilter: filter,
		})
		jobsOrder.InitializeWithJobs(ssn.ClusterInfo.PodGroupInfos)
		var names []string
		for !jobsOrder.IsEmpty() {
			names = append(names, jobsOrder.PopNextJob().Name)
		}
		return names
	}

	assert.ElementsMatch(t, []string{"pending"}, popAll(nil), "nil filter drops jobs without pending pods")
	assert.ElementsMatch(t, []string{"pending"}, popAll(func(*podgroup_info.PodGroupInfo) bool { return false }))
	assert.ElementsMatch(t, []string{"pending", "running"}, popAll(func(*podgroup_info.PodGroupInfo) bool { return true }),
		"kept jobs still go through the other filters, so the job of a missing queue is dropped")
	assert.ElementsMatch(t, []string{"pending", "running"}, popAll(func(job *podgroup_info.PodGroupInfo) bool {
		return job.Name != "pending"
	}), "the filter only decides for jobs without pending pods")
}
