// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestPipelineOnlyFailureDoesNotMutateFitErrors(t *testing.T) {
	task := &pod_info.PodInfo{
		UID:       "task",
		Job:       "job",
		Name:      "task",
		Namespace: "namespace",
	}
	job := podgroup_info.NewPodGroupInfo(task.Job, task)
	existingTaskError := common_info.NewFitErrors()
	existingTaskError.SetError("existing task error")
	job.SetTaskFitErrors(task, existingTaskError)
	existingJobError := common_info.NewJobFitError(
		job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
		podgroup_info.PodSchedulingErrors, []string{"existing job error"},
	)
	job.AddJobFitError(existingJobError)

	clusterInfo := api.NewClusterInfo()
	clusterInfo.PodGroupInfos[job.UID] = job
	ssn := &framework.Session{ClusterInfo: clusterInfo}
	ssn.AddPrePredicateFn(func(*pod_info.PodInfo, *podgroup_info.PodGroupInfo) error {
		return errors.New("simulation-only failure")
	})

	outcome := allocateTasksOnNodeSet(ssn, ssn.Statement(), nil, job, []*pod_info.PodInfo{task}, true)

	require.False(t, outcome.success)
	require.Same(t, existingTaskError, job.TasksFitErrors[task.UID])
	require.Equal(t, []common_info.JobFitError{existingJobError}, job.JobFitErrors)
}

func TestAllocationResultPublishesAndClearsFitErrors(t *testing.T) {
	task := &pod_info.PodInfo{UID: "task", Job: "job"}
	job := podgroup_info.NewPodGroupInfo(task.Job, task)
	taskFitError := common_info.NewFitErrors()
	taskFitError.SetError("allocation failed")
	jobFitError := common_info.NewJobFitError(
		job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
		podgroup_info.PodSchedulingErrors, []string{"allocation failed"},
	)

	failed := AllocationResult{
		Success: false,
		diagnostics: &allocationDiagnostics{
			failedTask:   task,
			taskFitError: taskFitError,
			jobFitErrors: []common_info.JobFitError{jobFitError},
		},
	}
	failed.PublishFitErrors(job)

	require.Same(t, taskFitError, job.TasksFitErrors[task.UID])
	require.Equal(t, []common_info.JobFitError{jobFitError}, job.JobFitErrors)

	AllocationResult{Success: true, diagnostics: &allocationDiagnostics{}}.PublishFitErrors(job)

	require.NotContains(t, job.TasksFitErrors, task.UID)
	require.Empty(t, job.JobFitErrors)
}

func TestSelectOutcomeByAllocatedTasks(t *testing.T) {
	first := allocationOutcome{allocatedTasks: 1, diagnostics: &allocationDiagnostics{}}
	second := allocationOutcome{allocatedTasks: 2, diagnostics: &allocationDiagnostics{}}

	require.Same(t, second.diagnostics, selectOutcomeByAllocatedTasks(first, second).diagnostics)
	require.Same(t, first.diagnostics, selectOutcomeByAllocatedTasks(first, first).diagnostics)
}
