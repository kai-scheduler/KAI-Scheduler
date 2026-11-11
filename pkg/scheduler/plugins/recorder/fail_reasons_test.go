// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestFailReasonsRecordsCodesOnly(t *testing.T) {
	job := &podgroup_info.PodGroupInfo{Name: "job-a", Namespace: "team"}
	job.AddSimpleJobFitError(enginev2alpha2.NonPreemptibleOverQuota, "queue team/job-a is over quota")
	taskErrors := common_info.NewFitErrors()
	taskErrors.SetError("no nodes with enough resources were found for team/job-a")
	job.TasksFitErrors = map[common_info.PodID]*common_info.TasksFitErrors{"pod": taskErrors}

	reasons := failReasons(&framework.Session{ClusterInfo: &api.ClusterInfo{
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"team/job-a": job},
	}})

	require.Equal(t, []recorder.FailReason{{
		Namespace: "team",
		Name:      "job-a",
		Reason:    string(enginev2alpha2.NonPreemptibleOverQuota),
	}}, reasons)
}
