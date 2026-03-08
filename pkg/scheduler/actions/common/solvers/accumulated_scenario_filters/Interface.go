// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import "github.com/kai-scheduler/KAI-Scheduler/pkg/scheduler/actions/common/solvers/scenario"

type Interface interface {
	Name() string
	Filter(*scenario.ByNodeScenario) (bool, error)
}
