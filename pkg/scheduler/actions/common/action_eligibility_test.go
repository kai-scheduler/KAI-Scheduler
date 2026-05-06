// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestVictimInvariantPrePredicateFailureForTasks(t *testing.T) {
	task1 := &pod_info.PodInfo{UID: common_info.PodID(types.UID("task-1")), Name: "task-1", Namespace: "ns1"}
	task2 := &pod_info.PodInfo{UID: common_info.PodID(types.UID("task-2")), Name: "task-2", Namespace: "ns1"}
	expectedErr := errors.New("missing dependency")

	var seenTasks []common_info.PodID
	ssn := &framework.Session{}
	ssn.AddVictimInvariantPrePredicateFn(func(task *pod_info.PodInfo) *api.VictimInvariantPrePredicateFailure {
		seenTasks = append(seenTasks, task.UID)
		if task.UID != task2.UID {
			return nil
		}
		return &api.VictimInvariantPrePredicateFailure{
			Err: expectedErr,
		}
	})

	blockedTask, failure := VictimInvariantPrePredicateFailureForTasks(ssn, []*pod_info.PodInfo{task1, task2})
	require.NotNil(t, failure)
	require.Same(t, task2, blockedTask)
	require.Same(t, expectedErr, failure.Err)
	require.Equal(t, []common_info.PodID{task1.UID, task2.UID}, seenTasks)
}

func TestRecordVictimInvariantPrePredicateFailure(t *testing.T) {
	job := podgroup_info.NewPodGroupInfo("job-1")
	job.Name = "job-1"
	job.Namespace = "ns1"
	task := &pod_info.PodInfo{UID: common_info.PodID(types.UID("task-1")), Name: "task-1", Namespace: "ns1"}
	failure := &api.VictimInvariantPrePredicateFailure{
		Err: errors.New("persistentvolumeclaim \"missing\" not found"),
	}

	RecordVictimInvariantPrePredicateFailure(job, task, failure)

	taskFitError, found := job.TasksFitErrors[task.UID]
	require.True(t, found)
	require.Contains(t, taskFitError.Error(), `persistentvolumeclaim "missing" not found`)
	require.Len(t, job.JobFitErrors, 1)
	require.Contains(t, job.JobFitErrors[0].DetailedMessage(), "Resources were not found for pod ns1/task-1 due to:")
}
