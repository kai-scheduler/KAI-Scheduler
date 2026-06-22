// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestAttemptToReclaimSkipsJobSolutionStartWhenActionBudgetAlreadyExhausted(t *testing.T) {
	queueID := common_info.QueueID("reclaim-queue")
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				queueID: {
					UID:  queueID,
					Name: string(queueID),
				},
			},
		},
		Config: &conf.SchedulerConfiguration{
			ScenarioSearchBudgets: &conf.ScenarioSearchBudgets{
				MaxActionSearchDuration: map[string]string{
					scenariosearch.ActionReclaim: "1ns",
				},
			},
		},
	}
	onJobSolutionStartCalls := 0
	ssn.AddOnJobSolutionStartFn(func() {
		onJobSolutionStartCalls++
	})

	actionBudget, err := solvers.NewActionSearchBudget(ssn, framework.Reclaim)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)

	job := podgroup_info.NewPodGroupInfo("reclaim-job")
	job.Name = "reclaim-job"
	job.Namespace = "runai-reclaim"
	job.Queue = queueID

	succeeded, statement, victims, result := New().attemptToReclaimForSpecificJob(ssn, job, actionBudget)

	require.False(t, succeeded)
	require.Nil(t, statement)
	require.Empty(t, victims)
	require.Equal(t, solvers.SearchResultNotAttempted, result.Reason())
	require.Zero(t, onJobSolutionStartCalls)
}
