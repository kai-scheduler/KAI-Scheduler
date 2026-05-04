// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

// Scenario is a candidate plan: pending tasks to allocate, and the victim
// set to evict. No "recorded" carry-forward, no per-node bucketing in the
// type — those are internal details of generator implementations.
type Scenario struct {
	// Preemptor is the job whose pending tasks we are trying to place.
	Preemptor *podgroup_info.PodGroupInfo

	// Pending are the pods of Preemptor to be placed in this scenario.
	Pending []*pod_info.PodInfo

	// Victims are the pods the simulator should evict.
	Victims []*pod_info.PodInfo

	// Candidates is the broader candidate set that an action validator
	// evaluates fair-share against. May be a strict superset of Victims
	// when the generator emits per-node subsets of a larger accumulated
	// pool. Empty Candidates means "use Victims" — kept lean for
	// generators that don't carry a separate notion.
	//
	// Phase-3 transitional: the legacy ScenarioInfo validators expect
	// the whole accumulated pool, so the accumulating generator
	// populates this with recorded ∪ all potentials. Phase 6 will give
	// validators native access to the SimulationResult and this field
	// can be removed.
	Candidates []*pod_info.PodInfo
}
