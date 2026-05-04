// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	solverscenario "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// LegacyValidator adapts the existing SolutionValidator (a function from
// api.ScenarioInfo to bool) into the v2.Validator interface.
//
// Phase 6 will replace the adapter by giving each action a native
// Validate(Scenario, SimulationResult) implementation. Until then, this
// shim lets reclaim/preempt/consolidation reuse their plugin-registered
// validators without modification.
//
// The adapter rebuilds a BaseScenario from the flat v2.Scenario so the
// legacy validator sees the same ScenarioInfo shape it always has. The
// SimulationResult is ignored — today's validators only inspect the
// scenario.
func LegacyValidator(
	ssn *framework.Session,
	name string,
	fn func(api.ScenarioInfo) bool,
) Validator {
	return &legacyValidator{ssn: ssn, name: name, fn: fn}
}

type legacyValidator struct {
	ssn  *framework.Session
	name string
	fn   func(api.ScenarioInfo) bool
}

func (v *legacyValidator) Name() string { return v.name }

func (v *legacyValidator) Validate(s Scenario, _ SimulationResult) bool {
	if v.fn == nil {
		return true
	}
	info := solverscenario.NewBaseScenario(v.ssn, s.Preemptor, s.Pending, s.Victims, nil)
	return v.fn(info)
}
