// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

// Solve drives the search loop. No outer gang loop, no per-node sub-loop,
// no fallbacks: filters are wrapped into the generator via WithFilters
// before Solve is called.
//
// Returns the first scenario whose simulation is feasible AND passes the
// validator. The generator's emission order determines which scenario
// wins among ties: earlier scenarios are preferred (and per Generator
// contract, earlier = less disruptive).
//
// On success the caller owns result.Statement and must Commit or Discard
// it. On failure result is the zero value.
func Solve(g Generator, sim Simulator, val Validator) (Scenario, SimulationResult, bool) {
	for {
		s, ok := g.Next()
		if !ok {
			return Scenario{}, SimulationResult{}, false
		}

		r := sim.Simulate(s)
		if !r.Feasible {
			continue
		}
		if val != nil && !val.Validate(s, r) {
			if r.Statement != nil {
				r.Statement.Discard()
			}
			continue
		}

		return s, r, true
	}
}
