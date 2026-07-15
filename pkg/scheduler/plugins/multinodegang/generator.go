// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package multinodegang

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type multiNodeGangGenerator struct {
	builder *solvers.PodAccumulatedScenarioBuilder
	first   bool
}

func NewMultiNodeGangGenerator(ctx framework.ScenarioGeneratorContext) framework.ScenarioGenerator {
	solveCtx, generateVictimsQueue, ok := solvers.ValidateScenarioGeneratorContext(ctx)
	if !ok {
		return nil
	}
	victimsQueue := generateVictimsQueue()
	if victimsQueue == nil {
		return nil
	}

	return &multiNodeGangGenerator{
		builder: solvers.NewPodAccumulatedScenarioBuilder(
			solveCtx.Session,
			solveCtx.PartialPendingJob,
			solveCtx.RecordedVictimsJobs,
			victimsQueue,
			solveCtx.FeasibleNodes,
		),
		first: true,
	}
}

func (g *multiNodeGangGenerator) Name() string {
	return constants.GeneratorMultiNodeGang
}

func (g *multiNodeGangGenerator) Next() api.ScenarioInfo {
	var sn *scenario.ByNodeScenario
	if g.first {
		g.first = false
		sn = g.builder.GetValidScenario()
	} else {
		sn = g.builder.GetNextScenario()
	}
	if sn == nil {
		return nil
	}
	return sn
}
