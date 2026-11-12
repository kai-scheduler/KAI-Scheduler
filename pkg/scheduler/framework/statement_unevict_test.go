// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestStatementUnevict(t *testing.T) {
	tests := []struct {
		name          string
		fn            func(t *testing.T, s *Statement, tasks []*pod_info.PodInfo)
		expectedValid []bool
		expectedState []pod_status.PodStatus
	}{
		{
			name: "undoes only the eviction of the given task",
			fn: func(t *testing.T, s *Statement, tasks []*pod_info.PodInfo) {
				evictForTest(t, s, tasks[0])
				evictForTest(t, s, tasks[1])
				evictForTest(t, s, tasks[2])
				require.NoError(t, s.Unevict(tasks[1]))
			},
			expectedValid: []bool{true, false, true, true},
			expectedState: []pod_status.PodStatus{pod_status.Releasing, pod_status.Running, pod_status.Releasing},
		},
		{
			name: "skips an eviction of the same task that was already undone",
			fn: func(t *testing.T, s *Statement, tasks []*pod_info.PodInfo) {
				evictForTest(t, s, tasks[0])
				require.NoError(t, s.Unevict(tasks[0]))
				evictForTest(t, s, tasks[0])
				require.NoError(t, s.Unevict(tasks[0]))
			},
			expectedValid: []bool{false, true, false, true},
			expectedState: []pod_status.PodStatus{pod_status.Running, pod_status.Running, pod_status.Running},
		},
		{
			name: "fails when the task has no eviction in effect",
			fn: func(t *testing.T, s *Statement, tasks []*pod_info.PodInfo) {
				evictForTest(t, s, tasks[0])
				require.NoError(t, s.Unevict(tasks[0]))
				assert.Error(t, s.Unevict(tasks[0]))
				assert.Error(t, s.Unevict(tasks[1]))
			},
			expectedValid: []bool{false, true},
			expectedState: []pod_status.PodStatus{pod_status.Running, pod_status.Running, pod_status.Running},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, tasks := newUnevictTestStatement()
			test.fn(t, s, tasks)

			actualValid := make([]bool, len(s.operations))
			for i := range s.operations {
				actualValid[i] = s.operationValid(i)
			}
			assert.Equal(t, test.expectedValid, actualValid)
			for i, status := range test.expectedState {
				assert.Equal(t, status, tasks[i].Status, "task %d", i)
			}
		})
	}
}

func newUnevictTestStatement() (*Statement, []*pod_info.PodInfo) {
	jobs := []*jobs_fake.TestJobBasic{
		{
			Name:                "running_job0",
			RequiredGPUsPerTask: 1,
			QueueName:           "queue0",
			Priority:            constants.PriorityTrainNumber,
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Running, NodeName: "node0"},
				{State: pod_status.Running, NodeName: "node0"},
				{State: pod_status.Running, NodeName: "node0"},
			},
		},
	}
	vectorMap := resource_info.NewResourceVectorMap()
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(
		map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 4}}, tasksToNodeMap, nil, vectorMap)
	ssn := &Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap}}

	podsByName := jobsInfoMap["running_job0"].GetAllPodsMap()
	var tasks []*pod_info.PodInfo
	for _, name := range []string{"running_job0-0", "running_job0-1", "running_job0-2"} {
		tasks = append(tasks, podsByName[common_info.PodID(name)])
	}

	return &Statement{ssn: ssn, sessionID: "1234"}, tasks
}

func evictForTest(t *testing.T, s *Statement, task *pod_info.PodInfo) {
	t.Helper()
	require.NoError(t, s.Evict(task, "evicting task",
		eviction_info.EvictionMetadata{Action: "action", EvictionGangSize: 1}))
}
