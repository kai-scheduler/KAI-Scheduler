// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fullnodefirst

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const Name = "sg-fullnodefirst"

type fullNodeFirstPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &fullNodeFirstPlugin{}
}

func (p *fullNodeFirstPlugin) Name() string {
	return Name
}

func (p *fullNodeFirstPlugin) OnSessionOpen(ssn *framework.Session) {
	addScenarioGenerator(ssn, constants.GeneratorFullNodeFirst, NewFullNodeFirstGenerator)
}

func (p *fullNodeFirstPlugin) OnSessionClose(_ *framework.Session) {}

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
