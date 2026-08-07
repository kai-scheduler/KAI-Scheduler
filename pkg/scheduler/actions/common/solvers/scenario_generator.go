// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

const scenarioSearchResultUnsolved = "unsolved"
const scenarioSearchResultValidatorRejected = "validator_rejected"
const scenarioStateDuplicate = "duplicate"

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
	ctx                      *SolveContext
	generators               []framework.ScenarioGenerator
	registrations            []framework.ScenarioGeneratorRegistration
	jobBudget                *jobSearchBudget
	currentIndex             int
	currentBudget            *generatorSearchBudget
	currentName              string
	currentStartedAt         time.Time
	resumeCursor             *scenarioFingerprint
	generatorBudgetExhausted bool
	stopReason               SearchResultReason
}

func newScenarioPortfolio(ctx *SolveContext, jobBudget *jobSearchBudget) *scenarioPortfolio {
	if ctx == nil || ctx.Session == nil {
		return &scenarioPortfolio{
			ctx:        ctx,
			jobBudget:  jobBudget,
			stopReason: SearchResultNoGenerator,
		}
	}
	return newScenarioPortfolioForAvailableGenerators(
		ctx, jobBudget,
		ctx.Session.ScenarioGeneratorRegistrations,
		nil,
	)
}

func newSingleGeneratorScenarioPortfolio(
	ctx *SolveContext,
	jobBudget *jobSearchBudget,
	availableGenerator framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
	checkpoint *framework.ScenarioCheckpoint,
) *scenarioPortfolio {
	portfolio := newScenarioPortfolioForAvailableGenerators(
		ctx, jobBudget, []framework.ScenarioGeneratorRegistration{availableGenerator}, generatorBudget,
	)
	if checkpoint != nil && len(portfolio.generators) == 1 &&
		portfolio.generators[0].Name() == checkpoint.GeneratorName {
		cursor := scenarioFingerprint(checkpoint.Cursor)
		portfolio.resumeCursor = &cursor
	}
	return portfolio
}

func newScenarioPortfolioForAvailableGenerators(
	ctx *SolveContext,
	jobBudget *jobSearchBudget,
	availableGenerators []framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
) *scenarioPortfolio {
	portfolio := &scenarioPortfolio{
		ctx:           ctx,
		jobBudget:     jobBudget,
		currentBudget: generatorBudget,
		stopReason:    SearchResultGeneratorsExhausted,
	}
	if ctx == nil || ctx.Session == nil {
		portfolio.stopReason = SearchResultNoGenerator
		return portfolio
	}

	for _, availableGenerator := range availableGenerators {
		if availableGenerator.Factory == nil {
			continue
		}
		generator := availableGenerator.Factory(ctx)
		if generator == nil {
			continue
		}
		portfolio.generators = append(portfolio.generators, generator)
		portfolio.registrations = append(portfolio.registrations, availableGenerator)
	}
	if len(portfolio.generators) == 0 {
		if len(availableGenerators) == 0 {
			portfolio.stopReason = SearchResultNoGenerator
		}
	}
	return portfolio
}

func (p *scenarioPortfolio) Next() *scenario.ByNodeScenario {
	for {
		generator := p.currentGenerator()
		if generator == nil {
			return nil
		}
		if p.currentBudget == nil {
			p.currentBudget = p.jobBudget.BeginGenerator(generator.Name())
		}
		if p.currentBudget.Exhausted() {
			p.generatorBudgetExhausted = true
			p.moveToNextGenerator()
			continue
		}

		generatorName := generator.Name()
		attemptStartedAt := time.Now()
		sn := generator.Next()
		byNodeScenario, ok := sn.(*scenario.ByNodeScenario)
		if sn != nil && !ok {
			p.observeGeneratorAttempt(generatorName, "unsupported", attemptStartedAt)
			log.InfraLogger.V(4).Infof(
				"Scenario generator <%s> returned unsupported scenario type %T",
				generatorName, sn,
			)
			p.moveToNextGenerator()
			continue
		}
		if byNodeScenario == nil {
			if p.resumeCursor != nil {
				p.restartCurrentGeneratorWithoutCheckpoint()
				continue
			}
			p.observeGeneratorAttempt(generatorName, string(SearchResultGeneratorsExhausted), attemptStartedAt)
			p.moveToNextGenerator()
			continue
		}
		if p.resumeCursor != nil {
			if fingerprintScenario(byNodeScenario) != *p.resumeCursor {
				continue
			}
			p.resumeCursor = nil
			p.currentBudget = nil
			continue
		}
		p.currentName = generatorName
		p.currentStartedAt = attemptStartedAt
		metrics.IncScenarioSearchScenario(p.ctx.ActionType, generatorName, "emitted")
		return byNodeScenario
	}
}

func (p *scenarioPortfolio) CurrentGeneratorName() string {
	if p == nil {
		return ""
	}
	return p.currentName
}

func (p *scenarioPortfolio) ObserveCurrentAttempt(result string) {
	if p == nil || p.currentStartedAt.IsZero() {
		return
	}
	p.observeGeneratorAttempt(p.currentName, result, p.currentStartedAt)
	p.currentStartedAt = time.Time{}
}

func (p *scenarioPortfolio) StopReason() SearchResultReason {
	if p == nil {
		return SearchResultNoGenerator
	}
	return p.stopReason
}

func (p *scenarioPortfolio) GeneratorBudgetExhausted() bool {
	return p != nil && p.generatorBudgetExhausted
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
	p.currentName = ""
	p.currentStartedAt = time.Time{}
}

func (p *scenarioPortfolio) restartCurrentGeneratorWithoutCheckpoint() {
	if p == nil {
		return
	}
	if p.currentIndex >= len(p.registrations) || p.ctx == nil {
		p.moveToNextGenerator()
		return
	}
	registration := p.registrations[p.currentIndex]
	if registration.Factory == nil {
		p.moveToNextGenerator()
		return
	}
	generator := registration.Factory(p.ctx)
	if generator == nil {
		p.moveToNextGenerator()
		return
	}
	p.generators[p.currentIndex] = generator
	p.resumeCursor = nil
}

func (p *scenarioPortfolio) observeGeneratorAttempt(generator string, result string, startedAt time.Time) {
	if p == nil || p.ctx == nil {
		return
	}
	metrics.ObserveScenarioSearchDuration(p.ctx.ActionType, generator, result, time.Since(startedAt))
}

// ValidateScenarioGeneratorContext extracts the solver context required by scenario generator plugins.
func ValidateScenarioGeneratorContext(ctx framework.ScenarioGeneratorContext) (*SolveContext, GenerateVictimsQueue, bool) {
	solveCtx, ok := ctx.(*SolveContext)
	if !ok || solveCtx == nil || solveCtx.Session == nil || solveCtx.Session.ClusterInfo == nil ||
		solveCtx.Session.ClusterInfo.Nodes == nil || solveCtx.Session.ClusterInfo.PodGroupInfos == nil ||
		solveCtx.PartialPendingJob == nil || solveCtx.FeasibleNodes == nil || solveCtx.GenerateVictimsQueue == nil {
		return nil, nil, false
	}

	return solveCtx, solveCtx.GenerateVictimsQueue, true
}
