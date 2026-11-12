// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
)

func TestCommitEventRecordsActionAndUnattemptedOps(t *testing.T) {
	stmt, task, _ := buildEvictMarkerSession()
	stmt.ssn.SetCurrentAction(string(Allocate))
	var got *CommitEvent
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		got = event
	}})
	task.NodeName = "missing-node"
	stmt.operations = []Operation{
		allocateOperation{taskInfo: task, nextNode: "node-a"},
		allocateOperation{taskInfo: task, nextNode: "node-b"},
	}

	err := stmt.Commit()
	require.Error(t, err)
	require.NotNil(t, got)
	require.Equal(t, string(Allocate), got.Action)
	require.Len(t, got.Ops, 2)
	require.Equal(t, "node-a", got.Ops[0].Node)
	require.False(t, got.Ops[0].NotAttempted)
	require.NotEmpty(t, got.Ops[0].Error)
	require.Equal(t, "node-b", got.Ops[1].Node)
	require.True(t, got.Ops[1].NotAttempted)
}

func TestDiscardDoesNotPublish(t *testing.T) {
	stmt, task, _ := buildEvictMarkerSession()
	stmt.ssn.SetCurrentAction(string(Reclaim))
	var got *CommitEvent
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		got = event
	}})
	require.NoError(t, stmt.Evict(task, "reclaim", eviction_info.EvictionMetadata{Action: string(Reclaim)}))

	stmt.Discard()

	require.Nil(t, got)
}

func TestUndoOpsAreNotPublished(t *testing.T) {
	stmt, task, node := buildEvictMarkerSession()
	controller := gomock.NewController(t)
	cacheMock := cache.NewMockCache(controller)
	cacheMock.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	stmt.ssn.Cache = cacheMock
	stmt.ssn.SetCurrentAction(string(Allocate))
	task.NodeName = node.Name
	var got *CommitEvent
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		got = event
	}})
	stmt.operations = []Operation{
		allocateOperation{taskInfo: task, nextNode: node.Name},
		undoOperation{operationIndex: 5, reverseOperation: func() error { return nil }},
	}

	require.NoError(t, stmt.Commit())
	require.NotNil(t, got)
	require.Equal(t, []string{allocate}, opKinds(got))
}

func opKinds(event *CommitEvent) []string {
	kinds := make([]string, 0, len(event.Ops))
	for _, op := range event.Ops {
		kinds = append(kinds, op.Kind)
	}
	return kinds
}

func TestNetZeroStatementPublishesNothing(t *testing.T) {
	stmt, task, _ := buildEvictMarkerSession()
	var commits int
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		commits++
	}})
	require.NoError(t, stmt.Evict(task, "reclaim", eviction_info.EvictionMetadata{Action: string(Reclaim)}))
	require.NoError(t, stmt.Unevict(task))
	require.NoError(t, stmt.Commit())
	require.Zero(t, commits)
}

func TestSessionEvictEmitsOneOp(t *testing.T) {
	stmt, task, _ := buildEvictMarkerSession()
	controller := gomock.NewController(t)
	cacheMock := cache.NewMockCache(controller)
	cacheMock.EXPECT().Evict(gomock.Any(), gomock.Any(), gomock.Any(), "stale").Return(nil)
	stmt.ssn.Cache = cacheMock
	stmt.ssn.SetCurrentAction(string(StaleGangEviction))
	var got *CommitEvent
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		got = event
	}})

	require.NoError(t, stmt.ssn.Evict(task, "stale", eviction_info.EvictionMetadata{Action: string(StaleGangEviction)}))

	require.NotNil(t, got)
	require.Equal(t, string(StaleGangEviction), got.Action)
	require.Len(t, got.Ops, 1)
	require.Equal(t, evict, got.Ops[0].Kind)
	require.Equal(t, task.Name, got.Ops[0].Name)
}
