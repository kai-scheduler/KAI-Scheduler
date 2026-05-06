// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	solverscenario "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// LegacyValidator adapts a func(api.ScenarioInfo) bool into a
// Validator. Used by actions that hold plugin-registered validators
// expecting the legacy ScenarioInfo shape (reclaim, preempt).
//
// The adapter rebuilds a BaseScenario from the flat Scenario so the
// legacy validator sees the same shape it always has. The
// SimulationResult is ignored — today's validators only inspect the
// scenario. Native Validators that read SimulationResult directly
// don't need this adapter.
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
	candidates := s.Candidates
	if candidates == nil {
		candidates = s.Victims
	}
	info := solverscenario.NewBaseScenario(v.ssn, s.Preemptor, s.Pending, candidates, nil)
	return v.fn(info)
}
