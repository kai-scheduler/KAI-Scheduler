// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

// Generator yields candidate scenarios. Implementations choose the search
// strategy (accumulation, multi-rooted, diversified, scoring).
//
// Contract: Scenarios are emitted in non-decreasing disruption order. The
// first valid scenario returned by Solve is therefore the least-disruptive
// solution found.
type Generator interface {
	// Next returns the next scenario, or false when the generator is exhausted.
	Next() (Scenario, bool)
}

// WithFilters wraps a generator so all yielded scenarios pass every filter.
// The result is itself a Generator (decorator pattern).
func WithFilters(g Generator, fs ...Filter) Generator {
	if len(fs) == 0 {
		return g
	}
	return &filteredGenerator{inner: g, filters: fs}
}

type filteredGenerator struct {
	inner   Generator
	filters []Filter
}

func (g *filteredGenerator) Next() (Scenario, bool) {
	for {
		s, ok := g.inner.Next()
		if !ok {
			return Scenario{}, false
		}
		if g.acceptAll(s) {
			return s, true
		}
	}
}

func (g *filteredGenerator) acceptAll(s Scenario) bool {
	for _, f := range g.filters {
		if !f.Accept(s) {
			return false
		}
	}
	return true
}
