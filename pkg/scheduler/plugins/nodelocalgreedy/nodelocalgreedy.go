// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodelocalgreedy

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const Name = "sg-nodelocalgreedy"

type nodeLocalGreedyPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &nodeLocalGreedyPlugin{}
}

func (p *nodeLocalGreedyPlugin) Name() string {
	return Name
}

func (p *nodeLocalGreedyPlugin) OnSessionOpen(ssn *framework.Session) {
	addScenarioGenerator(ssn, constants.GeneratorNodeLocalGreedy, NewNodeLocalGreedyGenerator)
}

func (p *nodeLocalGreedyPlugin) OnSessionClose(_ *framework.Session) {}

func addScenarioGenerator(
	ssn *framework.Session, name string, factory framework.ScenarioGeneratorFactory,
) {
	for _, registration := range ssn.ScenarioGeneratorRegistrations {
		if registration.Name == name {
			return
		}
	}
	ssn.AddScenarioGenerator(name, factory)
}
