// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// TestSolveWithoutConsolidationDoesNotCommitCrossNodeVictims covers #2426 at the
// by-pod solver layer: with allowVictimConsolidation off, victims whose original
// node is outside the preemptor's placement are restored (not returned as victims).
func TestSolveWithoutConsolidationDoesNotCommitCrossNodeVictims(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-a": 1, "node-b": 1})
	require.NoError(t, ssn.InitNodeScoringPool())

	_, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-a", "node-b")
	setGeneratorTestMinAvailable(ssn.ClusterInfo.PodGroupInfos[victimTasks[0].Job], 1)

	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation)

	sn := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
	solver := newByPodSolver(map[string]*node_info.NodeInfo{}, nil, false, framework.Preempt)

	result := solver.solve(ssn, sn)
	require.True(t, result.solved, "scenario with both nodes' victims should still solve for the preemptor")
	require.NotNil(t, result.statement)
	defer result.statement.Discard()

	keptUIDs := map[common_info.PodID]struct{}{}
	for _, victim := range result.victimsTasks {
		keptUIDs[victim.UID] = struct{}{}
	}

	var victimOnA, victimOnB = victimTasks[0], victimTasks[1]
	require.Contains(t, keptUIDs, victimOnA.UID, "victim on the preemptor's node must stay evicted")
	require.NotContains(t, keptUIDs, victimOnB.UID, "victim on an unused node must not be committed as a victim")

	require.Equal(t, pod_status.Releasing, victimOnA.Status,
		"kept victim must be Releasing on its original node (simulation pipelines are dropped)")
	require.Equal(t, "node-a", victimOnA.NodeName)

	require.Equal(t, pod_status.Running, victimOnB.Status,
		"unused-node victim must be restored to Running before commit")
	require.Equal(t, "node-b", victimOnB.NodeName)

	require.True(t, pod_status.IsActiveAllocatedStatus(pendingJob.GetAllPodsMap()[pendingTasks[0].UID].Status) ||
		pendingJob.GetAllPodsMap()[pendingTasks[0].UID].Status == pod_status.Pipelined,
		"preemptor must still be placed")
}
