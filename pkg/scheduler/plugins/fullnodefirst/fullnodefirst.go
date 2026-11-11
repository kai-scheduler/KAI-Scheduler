// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fullnodefirst

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	Name = "sg-fullnodefirst"
	// GeneratorName is the scenario generator name used for registration, budgets and metrics.
	GeneratorName = "FullNodeFirst"

	// ThresholdArgument is the plugin argument holding the fraction of a node's capacity,
	// on at least one resource, that a task must request to be treated as a whole-node task.
	ThresholdArgument = "fullNodeThreshold"
	// DefaultFullNodeThreshold requires a task to request the node's full capacity on one resource.
	DefaultFullNodeThreshold = 1.0
	// minFullNodeThreshold is exclusive: above half a node, two tasks can never share one node.
	minFullNodeThreshold = 0.5
)

type fullNodeFirstPlugin struct {
	threshold float64
}

func New(arguments framework.PluginArguments) framework.Plugin {
	return &fullNodeFirstPlugin{threshold: parseThreshold(arguments)}
}

func parseThreshold(arguments framework.PluginArguments) float64 {
	threshold, err := arguments.GetFloat64(ThresholdArgument, DefaultFullNodeThreshold)
	if err != nil {
		log.InfraLogger.Warningf("Failed to parse %s: %v. Using default value of %v",
			ThresholdArgument, err, DefaultFullNodeThreshold)
		return DefaultFullNodeThreshold
	}
	if !(threshold > minFullNodeThreshold && threshold <= DefaultFullNodeThreshold) {
		log.InfraLogger.Warningf("%s must be greater than %v and at most %v, got %v. Using default value of %v",
			ThresholdArgument, minFullNodeThreshold, DefaultFullNodeThreshold, threshold, DefaultFullNodeThreshold)
		return DefaultFullNodeThreshold
	}
	return threshold
}

func (p *fullNodeFirstPlugin) Name() string {
	return Name
}

func (p *fullNodeFirstPlugin) OnSessionOpen(ssn *framework.Session) {
	addScenarioGenerator(ssn, GeneratorName, NewFullNodeFirstGeneratorFactory(p.threshold))
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
