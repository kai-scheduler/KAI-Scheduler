// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	. "go.uber.org/mock/gomock"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/consolidation"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestConsolidationSkipsSolverForVictimInvariantPrePredicateFailure(t *testing.T) {
	test_utils.InitTestingInfrastructure()
	controller := NewController(t)
	defer controller.Finish()

	ssn := test_utils.BuildSession(test_utils.TestTopologyBasic{
		Name: "consolidation victim invariant blocker",
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name:                "running-job-0",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node0",
					State:    pod_status.Running,
				}},
			},
			{
				Name:                "running-job-1",
				RequiredGPUsPerTask: 2,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					NodeName: "node1",
					State:    pod_status.Running,
				}},
			},
			{
				Name:                "pending-job",
				RequiredGPUsPerTask: 3,
				Priority:            constants.PriorityTrainNumber,
				QueueName:           "queue0",
				Tasks: []*tasks_fake.TestTaskBasic{{
					State: pod_status.Pending,
				}},
			},
		},
		Nodes: map[string]nodes_fake.TestNodeBasic{
			"node0": {GPUs: 4},
			"node1": {GPUs: 4},
		},
		Queues: []test_utils.TestQueueBasic{{
			Name:         "queue0",
			DeservedGPUs: 2,
		}},
	}, controller)

	job := ssn.PodGroupInfos[common_info.PodGroupID("pending-job")]
	task := job.PodInfos[common_info.PodID("pending-job-0")]
	blockerErr := errors.New("blocked before consolidation")
	calls := 0
	ssn.AddVictimInvariantPrePredicateFn(func(gotTask *pod_info.PodInfo) *api.VictimInvariantPrePredicateFailure {
		calls++
		require.Same(t, task, gotTask)
		return &api.VictimInvariantPrePredicateFailure{
			Err: blockerErr,
		}
	})

	consolidation.New().Execute(ssn)

	require.Positive(t, calls)
	require.Equal(t, pod_status.Pending, task.Status)
	require.Contains(t, job.NodesFitErrors[task.UID].Error(), blockerErr.Error())
	require.NotEmpty(t, job.JobFitErrors)
	require.Contains(t, job.JobFitErrors[0].Message, "Resources were not found for pod /pending-job-0 due to:")
}
