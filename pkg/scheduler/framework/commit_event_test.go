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

func TestDiscardEventCountsOpsOnly(t *testing.T) {
	stmt, task, _ := buildEvictMarkerSession()
	stmt.ssn.SetCurrentAction(string(Reclaim))
	var got *CommitEvent
	stmt.ssn.AddEventHandler(&EventHandler{OnCommit: func(event *CommitEvent) {
		got = event
	}})
	require.NoError(t, stmt.Evict(task, "reclaim", eviction_info.EvictionMetadata{Action: string(Reclaim)}))

	stmt.Discard()

	require.NotNil(t, got)
	require.Equal(t, 1, got.DiscardedOpCount)
	require.Empty(t, got.Ops)
	require.Equal(t, string(Reclaim), got.Action)
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
