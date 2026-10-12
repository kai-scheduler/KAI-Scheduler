// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestMatchExpectedAndRealTasksWithSameNamedJobsInDifferentNamespaces(t *testing.T) {
	metadata := TestTopologyBasic{
		Jobs: []*jobs_fake.TestJobBasic{
			{
				Name: "training", Namespace: "team-a", RequiredGPUsPerTask: 1,
				Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
			{
				Name: "training", Namespace: "team-b", RequiredGPUsPerTask: 2,
				Tasks: []*tasks_fake.TestTaskBasic{{State: pod_status.Pending}},
			},
		},
		JobExpectedResults: map[string]TestExpectedResultBasic{
			"team-a/training": {Status: pod_status.Pending, GPUsRequired: 1},
			"team-b/training": {Status: pod_status.Pending, GPUsRequired: 2},
		},
	}
	jobs, _, _ := jobs_fake.BuildJobsAndTasksMaps(metadata.Jobs, resource_info.NewResourceVectorMap())
	session := &framework.Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: jobs}}

	MatchExpectedAndRealTasks(t, 0, metadata, session)
}
