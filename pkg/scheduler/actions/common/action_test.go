// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/scheduler_util"
)

func TestTaskAllocationModeForPipelining(t *testing.T) {
	preemptor := podgroup_info.NewPodGroupInfo("preemptor")
	victim := podgroup_info.NewPodGroupInfo("victim")

	require.Equal(t, podgroup_info.SimulatedTaskAllocation,
		taskAllocationModeForPipelining(preemptor, preemptor, podgroup_info.SimulatedTaskAllocation))
	require.Equal(t, podgroup_info.PartialTaskAllocation,
		taskAllocationModeForPipelining(preemptor, preemptor, podgroup_info.PartialTaskAllocation))
	require.Equal(t, podgroup_info.VictimReallocation,
		taskAllocationModeForPipelining(victim, preemptor, podgroup_info.PartialTaskAllocation))
}

func TestGetJobsToAllocateOrdersOnlyScenarioParticipants(t *testing.T) {
	tests := []struct {
		name                string
		preemptorPriority   int32
		victimJobPriority   int32
		expectedJobsInOrder []string
	}{
		{
			name:                "preemptor before lower priority victim",
			preemptorPriority:   100,
			victimJobPriority:   1,
			expectedJobsInOrder: []string{"preemptor", "victim-0"},
		},
		{
			name:                "higher priority victim before preemptor",
			preemptorPriority:   1,
			victimJobPriority:   100,
			expectedJobsInOrder: []string{"victim-0", "preemptor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn, preemptor, victimTasks := newScenarioValidationSession(
				1_000, 1, tt.preemptorPriority, tt.victimJobPriority)

			jobsToAllocate := GetJobsToAllocate(ssn, victimTasks, preemptor)
			require.Equal(t, 2, jobsToAllocate.Len())
			require.Equal(t, tt.expectedJobsInOrder, drainJobs(jobsToAllocate))

			referenceJobsToAllocate := getJobsToAllocateFromAllPending(ssn, victimTasks, preemptor)
			require.Equal(t, tt.expectedJobsInOrder,
				drainScenarioParticipants(referenceJobsToAllocate, preemptor.UID, victimTasks))
		})
	}
}

func TestGetJobsToAllocateDeduplicatesVictimJobs(t *testing.T) {
	ssn, preemptor, victimTasks := newScenarioValidationSession(10, 1, 100, 1)
	victimJob := ssn.ClusterInfo.PodGroupInfos[victimTasks[0].Job]
	secondVictimTask := newScenarioValidationTask(victimJob.UID, "victim-0-second", pod_status.Allocated)
	victimJob.AddTaskInfo(secondVictimTask)

	jobsToAllocate := GetJobsToAllocate(ssn, append(victimTasks, secondVictimTask), preemptor)
	require.Equal(t, 2, jobsToAllocate.Len())
	require.Equal(t, []string{"preemptor", "victim-0"}, drainJobs(jobsToAllocate))
}

func newScenarioValidationSession(
	pendingJobs, victimJobs int, preemptorPriority, victimJobPriority int32,
) (*framework.Session, *podgroup_info.PodGroupInfo, []*pod_info.PodInfo) {
	ssn := &framework.Session{
		ClusterInfo: api.NewClusterInfo(),
		JobOrderFns: []common_info.CompareFn{func(l, r interface{}) int {
			left := l.(*podgroup_info.PodGroupInfo)
			right := r.(*podgroup_info.PodGroupInfo)
			switch {
			case left.Priority > right.Priority:
				return -1
			case left.Priority < right.Priority:
				return 1
			default:
				return 0
			}
		}},
	}
	ssn.ClusterInfo.Queues["default"] = &queue_info.QueueInfo{UID: "default", Name: "default"}

	preemptor, _ := newScenarioValidationJob("preemptor", preemptorPriority, pod_status.Pending)
	ssn.ClusterInfo.PodGroupInfos[preemptor.UID] = preemptor

	for index := 1; index < pendingJobs; index++ {
		job, _ := newScenarioValidationJob(fmt.Sprintf("unrelated-%d", index), 50, pod_status.Pending)
		ssn.ClusterInfo.PodGroupInfos[job.UID] = job
	}

	victimTasks := make([]*pod_info.PodInfo, 0, victimJobs)
	for index := 0; index < victimJobs; index++ {
		job, task := newScenarioValidationJob(fmt.Sprintf("victim-%d", index), victimJobPriority, pod_status.Allocated)
		ssn.ClusterInfo.PodGroupInfos[job.UID] = job
		victimTasks = append(victimTasks, task)
	}

	return ssn, preemptor, victimTasks
}

func newScenarioValidationJob(name string, priority int32, status pod_status.PodStatus) (*podgroup_info.PodGroupInfo, *pod_info.PodInfo) {
	jobID := common_info.PodGroupID(name)
	task := newScenarioValidationTask(jobID, name+"-pod", status)
	job := podgroup_info.NewPodGroupInfo(jobID, task)
	job.Name = name
	job.Queue = "default"
	job.Priority = priority
	return job, task
}

func newScenarioValidationTask(
	jobID common_info.PodGroupID, name string, status pod_status.PodStatus,
) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		UID:    common_info.PodID(name),
		Job:    jobID,
		Name:   name,
		Status: status,
	}
}

func getJobsToAllocateFromAllPending(
	ssn *framework.Session, preempteeTasks []*pod_info.PodInfo, preemptor *podgroup_info.PodGroupInfo,
) *utils.JobsOrderByQueues {
	jobsToAllocate := utils.GetAllPendingJobs(ssn)
	for _, task := range preempteeTasks {
		victimJob := ssn.ClusterInfo.PodGroupInfos[task.Job]
		jobsToAllocate[victimJob.UID] = victimJob
	}
	jobsToAllocate[preemptor.UID] = preemptor
	jobsToAllocateQueue := utils.NewJobsOrderByQueues(
		ssn, utils.JobsOrderInitOptions{MaxJobsQueueDepth: scheduler_util.QueueCapacityInfinite})
	jobsToAllocateQueue.InitializeWithJobs(jobsToAllocate)
	return &jobsToAllocateQueue
}

func drainJobs(jobsToAllocate *utils.JobsOrderByQueues) []string {
	jobs := make([]string, 0, jobsToAllocate.Len())
	for !jobsToAllocate.IsEmpty() {
		jobs = append(jobs, jobsToAllocate.PopNextJob().Name)
	}
	return jobs
}

func drainScenarioParticipants(
	jobsToAllocate *utils.JobsOrderByQueues,
	preemptorID common_info.PodGroupID,
	victimTasks []*pod_info.PodInfo,
) []string {
	victimJobIDs := make(map[common_info.PodGroupID]struct{}, len(victimTasks))
	for _, task := range victimTasks {
		victimJobIDs[task.Job] = struct{}{}
	}

	jobs := make([]string, 0, len(victimJobIDs)+1)
	for !jobsToAllocate.IsEmpty() {
		job := jobsToAllocate.PopNextJob()
		if _, isVictimJob := victimJobIDs[job.UID]; isVictimJob || job.UID == preemptorID {
			jobs = append(jobs, job.Name)
		}
	}
	return jobs
}
