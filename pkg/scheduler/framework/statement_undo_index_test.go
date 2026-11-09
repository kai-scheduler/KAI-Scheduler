// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestStatementUndoIndex(t *testing.T) {
	tests := []struct {
		name          string
		fn            func(t *testing.T, s *Statement, tasks undoIndexTasks)
		expectedValid []bool
		expectedState map[string]pod_status.PodStatus
	}{
		{
			name: "undo",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				evictForTest(t, s, tasks.running[1])
				require.NoError(t, s.Unevict(tasks.running[1]))
			},
			expectedValid: []bool{true, false, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Releasing,
				"running_job0-1": pod_status.Running,
			},
		},
		{
			name: "undo of an undo",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				require.NoError(t, s.Unevict(tasks.running[0]))
				// Re-applies the eviction (appended as a new evict) and appends an undo of the undo.
				require.NoError(t, s.undoOperation(1))
			},
			expectedValid: []bool{true, false, true, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Releasing,
			},
		},
		{
			name: "unevict skips an already undone eviction of the same task",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				require.NoError(t, s.Unevict(tasks.running[0]))
				evictForTest(t, s, tasks.running[0])
				require.NoError(t, s.Unevict(tasks.running[0]))
			},
			expectedValid: []bool{false, true, false, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Running,
			},
		},
		{
			name: "rollback drops undos of operations before the checkpoint",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				cp := s.Checkpoint()
				require.NoError(t, s.Unevict(tasks.running[0]))
				evictForTest(t, s, tasks.running[1])
				require.NoError(t, s.Rollback(cp))
			},
			expectedValid: []bool{true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Releasing,
				"running_job0-1": pod_status.Running,
			},
		},
		{
			name: "undo after rollback is tracked again",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				cp := s.Checkpoint()
				require.NoError(t, s.Unevict(tasks.running[0]))
				require.NoError(t, s.Rollback(cp))
				require.NoError(t, s.Unevict(tasks.running[0]))
			},
			expectedValid: []bool{false, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Running,
			},
		},
		{
			name: "rollback keeps undos before the checkpoint",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				require.NoError(t, s.Unevict(tasks.running[0]))
				cp := s.Checkpoint()
				evictForTest(t, s, tasks.running[1])
				require.NoError(t, s.Unevict(tasks.running[1]))
				require.NoError(t, s.Rollback(cp))
			},
			expectedValid: []bool{false, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Running,
				"running_job0-1": pod_status.Running,
			},
		},
		{
			name: "discard clears the index",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				evictForTest(t, s, tasks.running[1])
				require.NoError(t, s.Unevict(tasks.running[0]))
				s.Discard()
				assert.Empty(t, s.undoneBy)

				evictForTest(t, s, tasks.running[2])
				require.NoError(t, s.Unevict(tasks.running[2]))
			},
			expectedValid: []bool{false, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Running,
				"running_job0-1": pod_status.Running,
				"running_job0-2": pod_status.Running,
			},
		},
		{
			name: "convert allocated to pipelined rebuilds the index",
			fn: func(t *testing.T, s *Statement, tasks undoIndexTasks) {
				evictForTest(t, s, tasks.running[0])
				require.NoError(t, s.Unevict(tasks.running[0]))
				require.NoError(t, s.Allocate(tasks.pending, "node0"))
				require.NoError(t, s.ConvertAllAllocatedToPipelined(tasks.pending.Job))
			},
			expectedValid: []bool{false, true, true},
			expectedState: map[string]pod_status.PodStatus{
				"running_job0-0": pod_status.Running,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, tasks := newUndoIndexTestStatement()
			test.fn(t, s, tasks)

			require.Len(t, s.undoneBy, len(s.operations))
			actualValid := make([]bool, len(s.operations))
			for i := range s.operations {
				actualValid[i] = s.operationValid(i)
				assert.Equal(t, scanOperationValid(s.operations, i), actualValid[i],
					"operation %d validity differs from a full scan", i)
			}
			assert.Equal(t, test.expectedValid, actualValid)

			for name, status := range test.expectedState {
				assert.Equal(t, status, tasks.byName[name].Status, "task %s", name)
			}
		})
	}
}

// scanOperationValid is the reference definition of validity: an operation is valid unless
// the first undo targeting it is itself valid.
func scanOperationValid(operations []Operation, i int) bool {
	for undoIndex, operation := range operations {
		undoOp, isUndo := operation.(undoOperation)
		if isUndo && undoOp.operationIndex == i {
			return !scanOperationValid(operations, undoIndex)
		}
	}
	return true
}

type undoIndexTasks struct {
	running []*pod_info.PodInfo
	pending *pod_info.PodInfo
	byName  map[string]*pod_info.PodInfo
}

func newUndoIndexTestStatement() (*Statement, undoIndexTasks) {
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
		{
			Name:                "pending_job0",
			RequiredGPUsPerTask: 1,
			QueueName:           "queue0",
			Priority:            constants.PriorityTrainNumber,
			Tasks: []*tasks_fake.TestTaskBasic{
				{State: pod_status.Pending},
			},
		},
	}
	vectorMap := resource_info.NewResourceVectorMap()
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(jobs, vectorMap)
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(
		map[string]nodes_fake.TestNodeBasic{"node0": {GPUs: 8}}, tasksToNodeMap, nil, vectorMap)
	ssn := &Session{ClusterInfo: &api.ClusterInfo{PodGroupInfos: jobsInfoMap, Nodes: nodesInfoMap}}

	tasks := undoIndexTasks{byName: map[string]*pod_info.PodInfo{}}
	for _, job := range jobsInfoMap {
		for name, task := range job.GetAllPodsMap() {
			tasks.byName[string(name)] = task
		}
	}
	for _, name := range []string{"running_job0-0", "running_job0-1", "running_job0-2"} {
		tasks.running = append(tasks.running, tasks.byName[name])
	}
	tasks.pending = tasks.byName["pending_job0-0"]

	return &Statement{ssn: ssn, sessionID: "1234"}, tasks
}

func evictForTest(t *testing.T, s *Statement, task *pod_info.PodInfo) {
	t.Helper()
	require.NoError(t, s.Evict(task, "evicting task",
		eviction_info.EvictionMetadata{Action: "action", EvictionGangSize: 1}))
}
