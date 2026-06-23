// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenariogenerators

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
		generatorName: constants.GeneratorNodeLocalGreedy,
		factory:       solvers.NewNodeLocalGreedyGenerator,
	}
}

func NewMultiNodeGang(_ framework.PluginArguments) framework.Plugin {
	return &scenarioGeneratorPlugin{
		pluginName:    MultiNodeGangName,
		generatorName: constants.GeneratorMultiNodeGang,
		factory:       solvers.NewMultiNodeGangGenerator,
	}
}

func (p *scenarioGeneratorPlugin) Name() string {
	return p.pluginName
}

func (p *scenarioGeneratorPlugin) OnSessionOpen(ssn *framework.Session) {
	addScenarioGenerator(ssn, p.generatorName, p.factory)
}

func (p *scenarioGeneratorPlugin) OnSessionClose(_ *framework.Session) {}

func addScenarioGenerator(
	ssn *framework.Session, name string, factory framework.ScenarioGeneratorFactory,
) {
	for _, registration := range ssn.ScenarioGeneratorRegistrations {
		if registration.Name == name {
			return
		}
	}
	ssn.AddScenarioGenerator(name, factory, framework.Reclaim, framework.Preempt, framework.Consolidation)
}
