// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// Simulator runs the expensive work for one scenario: evict the victim set,
// place the pending pods (predicates), compute pipelined vs preempted
// disposition.
//
// Contract: pure from the caller's perspective — same scenario in → same
// result out. Implementations mutate session state internally via
// checkpoints and roll back before returning.
//
// On Feasible == true, the implementation MUST also return a committed
// Statement that the caller can use to make the simulation real, or
// discard. On Feasible == false, Statement is nil.
type Simulator interface {
	Simulate(Scenario) SimulationResult
}

// SimulationResult is the outcome of simulating one scenario.
type SimulationResult struct {
	// Feasible is true when every pending task was placed.
	Feasible bool

	// Placement maps each pending task to the node it landed on.
	// Only populated when Feasible.
	Placement map[*pod_info.PodInfo]*node_info.NodeInfo

	// Preempted: victims whose slot was actually taken by a pending pod
	// (real eviction).
	Preempted []*pod_info.PodInfo

	// Pipelined: victims that would have been evicted but ended up
	// re-homed or kept their slot (no real displacement).
	Pipelined []*pod_info.PodInfo

	// Statement is the simulation's pending session mutation. Non-nil
	// only when Feasible. Caller must Commit or Discard.
	Statement *framework.Statement
}
