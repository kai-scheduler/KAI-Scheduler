// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenariogenerators

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const (
	NodeLocalGreedyName = "sg-nodelocalgreedy"
	MultiNodeGangName   = "sg-multinodegang"
)

type scenarioGeneratorPlugin struct {
	pluginName    string
	generatorName string
	factory       framework.ScenarioGeneratorFactory
}

func NewNodeLocalGreedy(_ framework.PluginArguments) framework.Plugin {
	return &scenarioGeneratorPlugin{
		pluginName:    NodeLocalGreedyName,
		generatorName: scenariosearch.GeneratorNodeLocalGreedy,
		factory:       solvers.NewNodeLocalGreedyGenerator,
	}
}

func NewMultiNodeGang(_ framework.PluginArguments) framework.Plugin {
	return &scenarioGeneratorPlugin{
		pluginName:    MultiNodeGangName,
		generatorName: scenariosearch.GeneratorMultiNodeGang,
		factory:       solvers.NewMultiNodeGangGenerator,
	}
}

func (p *scenarioGeneratorPlugin) Name() string {
	return p.pluginName
}

func (p *scenarioGeneratorPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddScenarioGenerator(p.generatorName, p.factory,
		framework.Reclaim, framework.Preempt, framework.Consolidation)
}

func (p *scenarioGeneratorPlugin) OnSessionClose(_ *framework.Session) {}
