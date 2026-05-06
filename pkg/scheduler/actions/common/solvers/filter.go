// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

// Filter rejects scenarios cheaply, before expensive simulation.
//
// Stateless: same scenario in → same answer. If a particular filter wants
// to memoize across calls, that's an internal optimization invisible to
// callers.
type Filter interface {
	// Accept returns true if the scenario should proceed to simulation.
	Accept(Scenario) bool
	// Name identifies the filter in logs and metrics.
	Name() string
}
