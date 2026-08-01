// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package multinodegang

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const Name = "sg-multinodegang"

type multiNodeGangPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &multiNodeGangPlugin{}
}

func (p *multiNodeGangPlugin) Name() string {
	return Name
}

func (p *multiNodeGangPlugin) OnSessionOpen(ssn *framework.Session) {
	addScenarioGenerator(ssn, constants.GeneratorMultiNodeGang, NewMultiNodeGangGenerator)
}

func (p *multiNodeGangPlugin) OnSessionClose(_ *framework.Session) {}

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
