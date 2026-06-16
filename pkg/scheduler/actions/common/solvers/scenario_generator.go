// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type SolveContext struct {
	Session              *framework.Session
	ActionType           framework.ActionType
	PartialPendingJob    *podgroup_info.PodGroupInfo
	RecordedVictimsJobs  []*podgroup_info.PodGroupInfo
	RecordedVictimsTasks []*pod_info.PodInfo
	GenerateVictimsQueue GenerateVictimsQueue
	VictimsQueue         *utils.JobsOrderByQueues
	FeasibleNodes        map[string]*node_info.NodeInfo
	ProbeK               int
}

func (ctx *SolveContext) Action() framework.ActionType {
	return ctx.ActionType
}

type scenarioPortfolio struct {
	ctx           *SolveContext
	generators    []framework.ScenarioGenerator
	jobBudget     *jobSearchBudget
	currentIndex  int
	currentBudget *generatorSearchBudget
	enteredSearch bool
	stopReason    SearchResultReason
}

func newScenarioPortfolio(ctx *SolveContext, jobBudget *jobSearchBudget) *scenarioPortfolio {
	portfolio := &scenarioPortfolio{
		ctx:        ctx,
		jobBudget:  jobBudget,
		stopReason: SearchResultGeneratorsExhausted,
	}
	if ctx == nil || ctx.Session == nil {
		portfolio.stopReason = SearchResultNoGenerator
		return portfolio
	}

	for _, registration := range ctx.Session.ScenarioGeneratorRegistrations {
		if !scenarioGeneratorAppliesToAction(registration, ctx.ActionType) || registration.Factory == nil {
			continue
		}
		generator := registration.Factory(ctx)
		if generator == nil {
			continue
		}
		portfolio.generators = append(portfolio.generators, generator)
	}
	if len(portfolio.generators) == 0 {
		portfolio.stopReason = SearchResultNoGenerator
	}
	return portfolio
}

func (p *scenarioPortfolio) Next() *scenario.ByNodeScenario {
	for {
		generator := p.currentGenerator()
		if generator == nil {
			return nil
		}
		if p.deadlineExhausted() {
			p.stopReason = SearchResultDeadlineExhausted
			return nil
		}
		if p.currentBudget == nil {
			p.currentBudget = p.jobBudget.BeginGenerator(generator.Name())
		}
		if p.currentBudget.Exhausted() {
			p.moveToNextGenerator()
			continue
		}

		sn := generator.Next()
		if p.deadlineExhausted() {
			p.stopReason = SearchResultDeadlineExhausted
			return nil
		}
		if p.currentBudget.Exhausted() {
			p.moveToNextGenerator()
			continue
		}
		if sn == nil {
			p.moveToNextGenerator()
			continue
		}
		byNodeScenario, ok := sn.(*scenario.ByNodeScenario)
		if !ok {
			log.InfraLogger.V(4).Infof(
				"Scenario generator <%s> returned unsupported scenario type %T",
				generator.Name(), sn,
			)
			p.moveToNextGenerator()
			continue
		}
		p.enteredSearch = true
		return byNodeScenario
	}
}

func (p *scenarioPortfolio) StopReason() SearchResultReason {
	if p == nil {
		return SearchResultNoGenerator
	}
	return p.stopReason
}

func (p *scenarioPortfolio) currentGenerator() framework.ScenarioGenerator {
	if p == nil || p.currentIndex >= len(p.generators) {
		return nil
	}
	return p.generators[p.currentIndex]
}

func (p *scenarioPortfolio) moveToNextGenerator() {
	p.currentIndex++
	p.currentBudget = nil
}

func (p *scenarioPortfolio) deadlineExhausted() bool {
	return p == nil || p.jobBudget == nil || p.jobBudget.Remaining() <= 0
}

func scenarioGeneratorAppliesToAction(
	registration framework.ScenarioGeneratorRegistration, action framework.ActionType,
) bool {
	if len(registration.Actions) == 0 {
		return true
	}
	_, applies := registration.Actions[action]
	return applies
}

func validateScenarioGeneratorContext(ctx framework.ScenarioGeneratorContext) (*SolveContext, GenerateVictimsQueue, bool) {
	solveCtx, ok := ctx.(*SolveContext)
	if !ok || solveCtx == nil || solveCtx.Session == nil || solveCtx.Session.ClusterInfo == nil ||
		solveCtx.Session.ClusterInfo.Nodes == nil || solveCtx.Session.ClusterInfo.PodGroupInfos == nil ||
		solveCtx.PartialPendingJob == nil || solveCtx.FeasibleNodes == nil || solveCtx.GenerateVictimsQueue == nil {
		return nil, nil, false
	}

	return solveCtx, solveCtx.GenerateVictimsQueue, true
}
