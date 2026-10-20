// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestByPodSolverPrefersVictimOriginalNode(t *testing.T) {
	tests := []struct {
		name                        string
		preferredNode               string
		victimCanStayOnOriginalNode bool
	}{
		{name: "original node has highest score", preferredNode: "node-b", victimCanStayOnOriginalNode: true},
		{name: "alternate node has highest score", preferredNode: "node-c", victimCanStayOnOriginalNode: true},
		{name: "original node is not feasible", preferredNode: "node-c", victimCanStayOnOriginalNode: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newGeneratorTestSession(t, map[string]int{"node-a": 1, "node-b": 1, "node-c": 1})
			require.NoError(t, ssn.InitNodeScoringPool())
			_, victimTasks := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-a")
			_, extraVictims := addGeneratorTestJob(t, ssn, 1, 30, "team-victim", "node-b")
			victimTasks = append(victimTasks, extraVictims...)
			pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
			ssn.AddPredicateFn(func(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
				if (task.Job == pendingJob.UID || task.UID == victimTasks[0].UID) && node.Name != "node-a" {
					return fmt.Errorf("task is pinned to node-a")
				}
				if task.UID == victimTasks[1].UID && !tt.victimCanStayOnOriginalNode && node.Name == "node-b" {
					return fmt.Errorf("victim cannot be reallocated to its original node")
				}
				return nil
			})
			ssn.AddNodeOrderFn(func(_ *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
				if node.Name == tt.preferredNode {
					return 100, nil
				}
				return 0, nil
			})
			pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
				podgroup_info.SimulatedTaskAllocation)
			sn := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
			solver := newByPodSolver(ssn.ClusterInfo.Nodes, nil, false, framework.Reclaim)
			result := solver.solve(ssn, sn)
			require.True(t, result.solved)
			require.NotNil(t, result.statement)
			defer result.statement.Discard()
			t.Logf("preferred=%s: victim originally on B now has simulated node=%s, status=%v; eviction candidates=%d",
				tt.preferredNode, victimTasks[1].NodeName, victimTasks[1].Status, len(result.victimsTasks))
			require.Contains(t, result.victimsTasks, victimTasks[0])
			if tt.victimCanStayOnOriginalNode {
				require.Equal(t, "node-b", victimTasks[1].NodeName)
				require.Equal(t, pod_status.Running, victimTasks[1].Status)
				require.NotContains(t, result.victimsTasks, victimTasks[1])
				require.Len(t, result.victimsTasks, 1)
			} else {
				require.Equal(t, "node-c", victimTasks[1].NodeName)
				require.Equal(t, pod_status.Pipelined, victimTasks[1].Status)
				require.Contains(t, result.victimsTasks, victimTasks[1])
			}
		})
	}
}

func TestByPodSolverKeepsGangVictimsTogether(t *testing.T) {
	ssn := newGeneratorTestSession(t, map[string]int{"node-a": 1, "node-b": 1, "node-c": 1})
	require.NoError(t, ssn.InitNodeScoringPool())
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-a", "node-b")
	setGeneratorTestMinAvailable(victimJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	ssn.AddPredicateFn(func(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
		if (task.Job == pendingJob.UID || task.UID == victimTasks[0].UID) && node.Name != "node-a" {
			return fmt.Errorf("task is pinned to node-a")
		}
		return nil
	})
	ssn.AddNodeOrderFn(func(_ *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		if node.Name == "node-c" {
			return 100, nil
		}
		return 0, nil
	})
	pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation)
	sn := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
	solver := newByPodSolver(ssn.ClusterInfo.Nodes, nil, false, framework.Reclaim)
	result := solver.solve(ssn, sn)
	require.True(t, result.solved)
	require.NotNil(t, result.statement)
	defer result.statement.Discard()

	// Losing the pinned member means the two-member gang cannot be restored;
	// the member on node-b must remain a victim even though node-b is available.
	require.ElementsMatch(t, victimTasks, result.victimsTasks)
}

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
	pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
		podgroup_info.SimulatedTaskAllocation)
	return ssn, scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victimTasks, nil)
}
