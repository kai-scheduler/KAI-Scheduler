// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestSolveRollsBackFeasibleNodeAdditionsOnValidatorRejection(t *testing.T) {
	ssn, sn := newByPodSolverRollbackTestScenario(t)
	feasibleNodes := map[string]*node_info.NodeInfo{}
	rejectAll := func(api.ScenarioInfo) bool { return false }
	solver := newByPodSolver(feasibleNodes, rejectAll, false, framework.Reclaim)

	result := solver.solve(ssn, sn)

	require.False(t, result.solved)
	require.Empty(t, feasibleNodes, "rejected scenario must not leak its victim nodes into the shared map")
}

// Positive control: the same scenario solves without a validator, proving the
// rejection test exercises the post-allocation validator path rather than an
// allocation failure.
func TestSolveSolvesSameScenarioWithoutValidator(t *testing.T) {
	ssn, sn := newByPodSolverRollbackTestScenario(t)
	solver := newByPodSolver(map[string]*node_info.NodeInfo{}, nil, false, framework.Reclaim)

	result := solver.solve(ssn, sn)

	require.True(t, result.solved)
	require.NotNil(t, result.statement)
	result.statement.Discard()
}

func newByPodSolverRollbackTestScenario(t *testing.T) (*framework.Session, *scenario.ByNodeScenario) {
	t.Helper()

	ssn := newGeneratorTestSession(t, map[string]int{"node-1": 1})
	require.NoError(t, ssn.InitNodeScoringPool())
	_, victimTasks := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-1")
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn, false)
	return ssn, scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
}
