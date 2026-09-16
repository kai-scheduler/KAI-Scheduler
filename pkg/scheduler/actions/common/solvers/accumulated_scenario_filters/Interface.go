// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

type Interface interface {
	Name() string
	Filter(AccumulatedScenarioInput) (bool, error)
}
