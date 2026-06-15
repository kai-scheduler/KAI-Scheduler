// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenariosearch

const (
	ActionDefault       = "default"
	ActionReclaim       = "reclaim"
	ActionPreempt       = "preempt"
	ActionConsolidation = "consolidation"

	GeneratorNodeLocalGreedy = "NodeLocalGreedy"
	GeneratorMultiNodeGang   = "MultiNodeGang"

	DefaultActionBudget    = "5s"
	DefaultJobBudget       = "250ms"
	DefaultMinJobBudget    = "0s"
	DefaultGeneratorBudget = "250ms"
	DefaultNodeLocalGreedy = "50ms"
	DefaultMultiNodeGang   = "250ms"
)
