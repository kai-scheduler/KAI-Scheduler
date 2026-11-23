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

func TestGPUGroupMatcher(t *testing.T) {
	type assignment struct {
		node     string
		actual   []string
		expected []string
	}
	tests := []struct {
		name        string
		assignments []assignment
		wantErr     bool
	}{
		{
			name: "same label on the same group",
			assignments: []assignment{
				{node: "node0", actual: []string{"g0"}, expected: []string{"0"}},
				{node: "node0", actual: []string{"g0"}, expected: []string{"0"}},
			},
		},
		{
			name: "same label split across groups",
			assignments: []assignment{
				{node: "node0", actual: []string{"g0"}, expected: []string{"0"}},
				{node: "node0", actual: []string{"g1"}, expected: []string{"0"}},
			},
			wantErr: true,
		},
		{
			name: "different labels merged onto one group",
			assignments: []assignment{
				{node: "node0", actual: []string{"g0"}, expected: []string{"0"}},
				{node: "node0", actual: []string{"g0"}, expected: []string{"1"}},
			},
			wantErr: true,
		},
		{
			name: "labels are scoped per node",
			assignments: []assignment{
				{node: "node0", actual: []string{"g0"}, expected: []string{"0"}},
				{node: "node1", actual: []string{"g1"}, expected: []string{"0"}},
			},
		},
		{
			name: "task holds more GPU groups than labels",
			assignments: []assignment{
				{node: "node0", actual: []string{"g0", "g1"}, expected: []string{"0"}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := newGPUGroupMatcher()
			var err error
			for _, a := range tt.assignments {
				if matchErr := matcher.match(a.node, a.actual, a.expected); matchErr != nil {
					err = matchErr
				}
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("match() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
