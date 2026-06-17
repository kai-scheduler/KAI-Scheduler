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

	DefaultActionBudget    = "20s"
	DefaultJobBudget       = "10s"
	DefaultMinJobBudget    = "0s"
	DefaultGeneratorBudget = "5s"
	DefaultNodeLocalGreedy = "5s"
	DefaultMultiNodeGang   = "5s"
)
