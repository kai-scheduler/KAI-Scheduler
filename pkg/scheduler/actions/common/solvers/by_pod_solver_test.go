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
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nominatednode"
)

func TestByPodSolverPrefersVictimOriginalNode(t *testing.T) {
	tests := []struct {
		name                        string
		preferredNode               string
		victimCanStayOnOriginalNode bool
		disableNominationPlugin     bool
		action                      framework.ActionType
	}{
		{name: "original node has highest score", preferredNode: "node-b", victimCanStayOnOriginalNode: true, action: framework.Reclaim},
		{name: "alternate node has highest score", preferredNode: "node-c", victimCanStayOnOriginalNode: true, action: framework.Reclaim},
		{name: "original node is not feasible", preferredNode: "node-c", victimCanStayOnOriginalNode: false, action: framework.Reclaim},
		{name: "preempt prefers original node", preferredNode: "node-c", victimCanStayOnOriginalNode: true, action: framework.Preempt},
		{name: "consolidation preserves existing nomination", preferredNode: "node-c", victimCanStayOnOriginalNode: true, action: framework.Consolidation},
		{name: "disabled plugin preserves node scores", preferredNode: "node-c", victimCanStayOnOriginalNode: true, disableNominationPlugin: true, action: framework.Reclaim},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := newGeneratorTestSession(t, map[string]int{"node-a": 1, "node-b": 1, "node-c": 1})
			require.NoError(t, ssn.InitNodeScoringPool())
			if !tt.disableNominationPlugin {
				nominatednode.New(nil).OnSessionOpen(ssn)
			}
			_, victimTasks := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-a")
			_, extraVictims := addGeneratorTestJob(t, ssn, 1, 30, "team-victim", "node-b")
			victimTasks = append(victimTasks, extraVictims...)
			originalPod := victimTasks[1].Pod
			originalPod.Status.NominatedNodeName = "node-c"
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
			solver := newByPodSolver(ssn.ClusterInfo.Nodes, nil, false, tt.action)
			result := solver.solve(ssn, sn)
			require.True(t, result.solved)
			require.NotNil(t, result.statement)
			defer result.statement.Discard()
			require.Same(t, originalPod, victimTasks[1].Pod)
			require.Equal(t, "node-c", originalPod.Status.NominatedNodeName)
			t.Logf("preferred=%s: victim originally on B now has simulated node=%s, status=%v; eviction candidates=%d",
				tt.preferredNode, victimTasks[1].NodeName, victimTasks[1].Status, len(result.victimsTasks))
			require.Contains(t, result.victimsTasks, victimTasks[0])
			if tt.victimCanStayOnOriginalNode && !tt.disableNominationPlugin && tt.action != framework.Consolidation {
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
	nominatednode.New(nil).OnSessionOpen(ssn)
	victimJob, victimTasks := addGeneratorTestJob(t, ssn, 2, 20, "team-victim", "node-a", "node-b")
	setGeneratorTestMinAvailable(victimJob, 2)
	pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
	ssn.AddTaskOrderFn(func(left, right interface{}) int {
		if left.(*pod_info.PodInfo).UID == right.(*pod_info.PodInfo).UID {
			return 0
		}
		if left.(*pod_info.PodInfo).UID == victimTasks[1].UID {
			return -1
		}
		return 1
	})
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
	for _, task := range victimTasks {
		require.Equal(t, pod_status.Releasing, task.Status)
		require.Equal(t, task.Pod.Spec.NodeName, task.NodeName)
		require.Empty(t, task.Pod.Status.NominatedNodeName)
	}
}

func TestByPodSolverNominationRespectsAllocationConstraints(t *testing.T) {
	for _, constraint := range []string{"quota", "node subsets"} {
		t.Run(constraint, func(t *testing.T) {
			ssn := newGeneratorTestSession(t, map[string]int{"node-a": 1, "node-b": 1})
			require.NoError(t, ssn.InitNodeScoringPool())
			nominatednode.New(nil).OnSessionOpen(ssn)
			_, victims := addGeneratorTestJob(t, ssn, 1, 20, "team-victim", "node-a")
			blockedJob, blockedTasks := addGeneratorTestJob(t, ssn, 1, 30, "team-victim", "node-b")
			victims = append(victims, blockedTasks...)
			pendingJob := addGeneratorTestPendingJob(t, ssn, 1, 10, "team-pending")
			ssn.AddPredicateFn(func(task *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
				if (task.Job == pendingJob.UID || task.UID == victims[0].UID) && node.Name != "node-a" {
					return fmt.Errorf("task is pinned to node-a")
				}
				return nil
			})
			if constraint == "quota" {
				ssn.IsJobOverCapacityFns = append(ssn.IsJobOverCapacityFns,
					func(job *podgroup_info.PodGroupInfo, _ []*pod_info.PodInfo) *api.SchedulableResult {
						return &api.SchedulableResult{IsSchedulable: job.UID != blockedJob.UID}
					})
			} else {
				ssn.AddSubsetNodesFn(func(job *podgroup_info.PodGroupInfo, _ *subgroup_info.SubGroupInfo,
					_ map[string]*subgroup_info.PodSet, _ []*pod_info.PodInfo, nodes node_info.NodeSet,
				) ([]node_info.NodeSet, error) {
					if job.UID == blockedJob.UID {
						return nil, nil
					}
					return []node_info.NodeSet{nodes}, nil
				})
			}
			pendingTasks := podgroup_info.GetTasksToAllocate(pendingJob, ssn.SubGroupOrderFn, ssn.TaskOrderFn,
				podgroup_info.SimulatedTaskAllocation)
			sn := scenario.NewByNodeScenario(ssn, pendingJob, pendingTasks, victims, nil)
			solver := newByPodSolver(ssn.ClusterInfo.Nodes, nil, false, framework.Reclaim)
			result := solver.solve(ssn, sn)
			require.True(t, result.solved)
			require.NotNil(t, result.statement)
			defer result.statement.Discard()
			require.Equal(t, pod_status.Releasing, blockedTasks[0].Status)
			require.Contains(t, result.victimsTasks, blockedTasks[0])
			require.Len(t, ssn.PredicateFns, 1)
		})
	}
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
