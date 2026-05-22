// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type VictimTaskCursor struct {
	Len int
}

type VictimTaskDelta struct {
	Tasks     []*pod_info.PodInfo
	Next      VictimTaskCursor
	Monotonic bool
}

type AccumulatedScenarioInput interface {
	Scenario() *scenario.ByNodeScenario
	PotentialVictimsSince(cursor VictimTaskCursor) VictimTaskDelta
	RecordedVictimsSince(cursor VictimTaskCursor) VictimTaskDelta
}

type MonotonicScenarioInput struct {
	scenario *scenario.ByNodeScenario
}

func NewMonotonicScenarioInput(scenario *scenario.ByNodeScenario) MonotonicScenarioInput {
	return MonotonicScenarioInput{scenario: scenario}
}

func (input MonotonicScenarioInput) Scenario() *scenario.ByNodeScenario {
	return input.scenario
}

func (input MonotonicScenarioInput) PotentialVictimsSince(cursor VictimTaskCursor) VictimTaskDelta {
	return monotonicVictimDelta(input.scenario.PotentialVictimsTasks(), cursor)
}

func (input MonotonicScenarioInput) RecordedVictimsSince(cursor VictimTaskCursor) VictimTaskDelta {
	return monotonicVictimDelta(input.scenario.RecordedVictimsTasks(), cursor)
}

type FullScanScenarioInput struct {
	scenario *scenario.ByNodeScenario
}

func NewFullScanScenarioInput(scenario *scenario.ByNodeScenario) FullScanScenarioInput {
	return FullScanScenarioInput{scenario: scenario}
}

func (input FullScanScenarioInput) Scenario() *scenario.ByNodeScenario {
	return input.scenario
}

func (input FullScanScenarioInput) PotentialVictimsSince(_ VictimTaskCursor) VictimTaskDelta {
	victims := input.scenario.PotentialVictimsTasks()
	return VictimTaskDelta{
		Tasks: victims,
		Next:  VictimTaskCursor{Len: len(victims)},
	}
}

func (input FullScanScenarioInput) RecordedVictimsSince(_ VictimTaskCursor) VictimTaskDelta {
	victims := input.scenario.RecordedVictimsTasks()
	return VictimTaskDelta{
		Tasks: victims,
		Next:  VictimTaskCursor{Len: len(victims)},
	}
}

func monotonicVictimDelta(victims []*pod_info.PodInfo, cursor VictimTaskCursor) VictimTaskDelta {
	next := VictimTaskCursor{Len: len(victims)}
	if cursor.Len > len(victims) {
		return VictimTaskDelta{
			Tasks: victims,
			Next:  next,
		}
	}
	return VictimTaskDelta{
		Tasks:     victims[cursor.Len:],
		Next:      next,
		Monotonic: true,
	}
}
