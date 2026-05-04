// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

// Validator runs action-specific checks on a successful simulation.
//
// Receives both the scenario and the simulation result so it can read
// either Preempted alone or Preempted ∪ Pipelined depending on the
// semantic the action wants.
type Validator interface {
	Validate(Scenario, SimulationResult) bool
	Name() string
}
