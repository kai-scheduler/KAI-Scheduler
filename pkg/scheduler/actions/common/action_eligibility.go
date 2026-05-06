// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func VictimInvariantPrePredicateFailureForTasks(
	ssn *framework.Session,
	tasks []*pod_info.PodInfo,
) (*pod_info.PodInfo, *api.VictimInvariantPrePredicateFailure) {
	for _, task := range tasks {
		if failure := ssn.VictimInvariantPrePredicateFailure(task); failure != nil {
			return task, failure
		}
	}

	return nil, nil
}

func RecordVictimInvariantPrePredicateFailure(
	job *podgroup_info.PodGroupInfo,
	task *pod_info.PodInfo,
	failure *api.VictimInvariantPrePredicateFailure,
) {
	fitErrors := common_info.NewFitErrors()
	fitErrors.SetError(failure.Err.Error())
	job.AddTaskFitErrors(task, fitErrors)
	job.AddSimpleJobFitError(
		podgroup_info.PodSchedulingErrors,
		fmt.Sprintf("Resources were not found for pod %s/%s due to: %s",
			task.Namespace, task.Name, fitErrors.Error()),
	)
}
