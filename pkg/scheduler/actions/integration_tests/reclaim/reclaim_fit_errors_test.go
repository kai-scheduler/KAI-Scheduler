// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/h2non/gock.v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestSchedulingCyclePreservesAllocateFitErrors(t *testing.T) {
	defer gock.Off()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ssn := test_utils.BuildSession(buildFailedGangReclaimTopology(), ctrl)
	ssn.AddNodeOrderFn(func(_ *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		if node.Name == "node0" {
			return 1000, nil
		}
		return 0, nil
	})
	failedPrePredicateCalls := 0
	ssn.AddPrePredicateFn(func(
		task *pod_info.PodInfo,
		_ *podgroup_info.PodGroupInfo,
	) error {
		if task.Job != common_info.PodGroupID("pending-gang") || task.Name != "pending-gang-0" {
			return nil
		}
		releasingTasks := 0
		for _, job := range ssn.ClusterInfo.PodGroupInfos {
			releasingTasks += len(job.PodStatusIndex[pod_status.Releasing])
		}
		if releasingTasks < 2 {
			return nil
		}
		failedPrePredicateCalls++
		return errors.New("simulated pre-predicate failure")
	})
	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})
	actions := manySingleGPUJobsSchedulingCycleActions()
	actions[0].Execute(ssn)

	job := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending-gang")]
	require.NotNil(t, job)
	require.NotEmpty(t, job.TasksFitErrors)
	require.NotEmpty(t, job.JobFitErrors)
	require.NotContains(t, job.TasksFitErrors, common_info.PodID("pending-gang-0"))
	require.Zero(t, failedPrePredicateCalls)

	allocateTaskErrors := taskFitErrorMessages(job)
	allocateJobErrors := jobFitErrorMessages(job)
	firstTaskFitErrorMutation := ""
	firstJobFitErrorMutation := ""
	for _, action := range actions[1:] {
		action.Execute(ssn)
		currentJob := ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending-gang")]
		if firstTaskFitErrorMutation == "" &&
			!assert.ObjectsAreEqual(allocateTaskErrors, taskFitErrorMessages(currentJob)) {
			firstTaskFitErrorMutation = string(action.Name())
		}
		if firstJobFitErrorMutation == "" &&
			!assert.ObjectsAreEqual(allocateJobErrors, jobFitErrorMessages(currentJob)) {
			firstJobFitErrorMutation = string(action.Name())
		}
	}

	job = ssn.ClusterInfo.PodGroupInfos[common_info.PodGroupID("pending-gang")]
	assert.Equalf(t, allocateTaskErrors, taskFitErrorMessages(job),
		"task fit errors produced by Allocate were first changed by %s", firstTaskFitErrorMutation)
	assert.Equalf(t, allocateJobErrors, jobFitErrorMessages(job),
		"job fit errors produced by Allocate were first changed by %s", firstJobFitErrorMutation)
	require.Len(t, job.PodStatusIndex[pod_status.Pending], 2)
	require.NotZero(t, onJobSolutionStartCalls)
	require.NotZero(t, failedPrePredicateCalls)
}

func buildFailedGangReclaimTopology() test_utils.TestTopologyBasic {
	return test_utils.TestTopologyBasic{
		Name: "failed gang reclaim preserves allocate fit errors",
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 1},
			"node1": {GPUs: 1},
		},
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "running-victim",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "running-queue",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node0",
					State:    pod_status.Releasing,
				}},
			},
			{
				Name:                "running-victim-2",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "running-queue",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node1",
					State:    pod_status.Running,
				}},
			},
			{
				Name:                "pending-gang",
				RequiredGPUsPerTask: 1,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "pending-queue",
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Pending},
					{State: pod_status.Pending},
				},
			},
		},
		Queues: []test_utils.TestQueueBasic{
			{Name: "running-queue", DeservedGPUs: 0, GPUOverQuotaWeight: 0},
			{Name: "pending-queue", DeservedGPUs: 2, GPUOverQuotaWeight: 0},
		},
		Mocks: &test_utils.TestMock{CacheRequirements: &test_utils.CacheMocking{}},
	}
}

func taskFitErrorMessages(job *podgroup_info.PodGroupInfo) map[common_info.PodID]string {
	messages := make(map[common_info.PodID]string, len(job.TasksFitErrors))
	for taskID, fitErrors := range job.TasksFitErrors {
		messages[taskID] = fitErrors.Error()
	}
	return messages
}

func jobFitErrorMessages(job *podgroup_info.PodGroupInfo) []string {
	messages := make([]string, 0, len(job.JobFitErrors))
	for _, fitError := range job.JobFitErrors {
		messages = append(messages, fitError.DetailedMessage())
	}
	return messages
}
