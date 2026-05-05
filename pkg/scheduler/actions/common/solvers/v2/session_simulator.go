// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// sessionSimulator is the production Simulator. It wraps the existing
// EvictAllPreemptees + TryToVirtuallyAllocatePreemptorAndGetVictims
// primitives so a Scenario can be evaluated end-to-end against the
// session.
//
// Each Simulate call is independent: state mutations happen inside a
// fresh Statement, which is either returned to the caller (on Feasible)
// or discarded (on infeasible). The simulator instance itself carries
// no per-scenario state.
type sessionSimulator struct {
	ssn                  *framework.Session
	initialFeasibleNodes []*node_info.NodeInfo
	actionType           framework.ActionType
}

// NewSessionSimulator constructs a Simulator bound to a session and an
// initial set of candidate nodes. The simulator augments that set per
// scenario with host nodes of the scenario's victims so the virtual
// allocator can consider those nodes once their slots free up.
func NewSessionSimulator(
	ssn *framework.Session,
	feasibleNodes []*node_info.NodeInfo,
	actionType framework.ActionType,
) Simulator {
	nodes := make([]*node_info.NodeInfo, len(feasibleNodes))
	copy(nodes, feasibleNodes)
	return &sessionSimulator{
		ssn:                  ssn,
		initialFeasibleNodes: nodes,
		actionType:           actionType,
	}
}

func (s *sessionSimulator) Simulate(scenario Scenario) SimulationResult {
	statement := s.ssn.Statement()

	if err := common.EvictAllPreemptees(
		s.ssn, scenario.Victims, scenario.Preemptor, statement, s.actionType,
	); err != nil {
		statement.Discard()
		return SimulationResult{}
	}

	nodes := s.candidateNodes(scenario.Victims)
	jobsToAllocate := common.GetJobsToAllocate(s.ssn, scenario.Victims, scenario.Preemptor)
	success, _ := common.TryToVirtuallyAllocatePreemptorAndGetVictims(
		s.ssn, statement, nodes, scenario.Preemptor, jobsToAllocate, scenario.Victims,
	)
	if !success {
		statement.Discard()
		return SimulationResult{}
	}

	return SimulationResult{
		Feasible:  true,
		Statement: statement,
		Placement: extractPlacement(scenario.Pending, s.ssn),
		Preempted: filterByStatus(scenario.Victims, pod_status.Releasing),
		Pipelined: filterByStatus(scenario.Victims, pod_status.Pipelined),
	}
}

// candidateNodes returns the initial feasible set augmented with any
// host node that holds one of the scenario's victims. Without this
// expansion the virtual allocator would not consider those nodes when
// placing the pending pods, even though their slots are about to free.
func (s *sessionSimulator) candidateNodes(victims []*pod_info.PodInfo) []*node_info.NodeInfo {
	seen := make(map[string]bool, len(s.initialFeasibleNodes)+len(victims))
	nodes := make([]*node_info.NodeInfo, 0, len(s.initialFeasibleNodes)+len(victims))
	for _, n := range s.initialFeasibleNodes {
		if seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		nodes = append(nodes, n)
	}
	for _, v := range victims {
		if v.NodeName == "" || seen[v.NodeName] {
			continue
		}
		node, ok := s.ssn.ClusterInfo.Nodes[v.NodeName]
		if !ok {
			continue
		}
		seen[v.NodeName] = true
		nodes = append(nodes, node)
	}
	return nodes
}

func extractPlacement(pending []*pod_info.PodInfo, ssn *framework.Session) map[*pod_info.PodInfo]*node_info.NodeInfo {
	placement := make(map[*pod_info.PodInfo]*node_info.NodeInfo, len(pending))
	for _, p := range pending {
		if p.NodeName == "" {
			continue
		}
		if node, ok := ssn.ClusterInfo.Nodes[p.NodeName]; ok {
			placement[p] = node
		}
	}
	return placement
}

func filterByStatus(tasks []*pod_info.PodInfo, status pod_status.PodStatus) []*pod_info.PodInfo {
	out := make([]*pod_info.PodInfo, 0, len(tasks))
	for _, t := range tasks {
		if t.Status == status {
			out = append(out, t)
		}
	}
	return out
}
