// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

func TestSolveDeferredResizeChecksCanReclaimResources(t *testing.T) {
	ssn, reclaimer, _, _ := newVictimQueueTestSession()
	node := &node_info.NodeInfo{Name: "node-0"}

	canReclaim := false
	var gotJob *podgroup_info.PodGroupInfo
	ssn.AddCanReclaimResourcesFn(func(job *podgroup_info.PodGroupInfo) bool {
		gotJob = job
		return canReclaim
	})
	solutionStarts := 0
	ssn.AddOnJobSolutionStartFn(func() { solutionStarts++ })

	stmt, result := solveDeferredResize(ssn, nil)(reclaimer, node)
	require.Nil(t, stmt)
	require.Nil(t, result, "the solver must not run")
	require.Zero(t, solutionStarts)
	require.Same(t, reclaimer, gotJob)

	canReclaim = true
	stmt, result = solveDeferredResize(ssn, nil)(reclaimer, node)
	require.Nil(t, stmt)
	require.NotNil(t, result, "the solver ran")
	require.Equal(t, 1, solutionStarts)
}

// A failed resize-only job has no demand and would make every later job in its queue look harder.
func TestReclaimForPendingTasksSkipsJobWithoutPendingTasks(t *testing.T) {
	ssn, _, running, _ := newVictimQueueTestSession()
	require.Zero(t, running.GetNumPendingTasks())
	reclaimChecks := 0
	ssn.AddCanReclaimResourcesFn(func(*podgroup_info.PodGroupInfo) bool {
		reclaimChecks++
		return true
	})

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}
	require.False(t, New().reclaimForPendingTasks(ssn, running, nil, smallestFailedJobsByQueue))
	require.Empty(t, smallestFailedJobsByQueue)
	require.Zero(t, reclaimChecks)
}
