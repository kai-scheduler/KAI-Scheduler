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

	DefaultActionBudget    = "5m"
	DefaultJobBudget       = "4m"
	DefaultMinJobBudget    = "0s"
	DefaultGeneratorBudget = "2m"
	DefaultNodeLocalGreedy = "30s"
	DefaultMultiNodeGang   = "2m"
)
