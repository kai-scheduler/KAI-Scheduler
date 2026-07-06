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
	ctx                   *SolveContext
	generators            []framework.ScenarioGenerator
	jobBudget             *jobSearchBudget
	currentIndex          int
	currentBudget         *generatorSearchBudget
	currentName           string
	currentStartedAt      time.Time
	stopReason            SearchResultReason
	dedupCache            *scenarioDedupCache
	currentFingerprint    scenarioFingerprint
	currentFingerprintSet bool
}

func newScenarioPortfolio(
	ctx *SolveContext, jobBudget *jobSearchBudget, dedupCache *scenarioDedupCache,
) *scenarioPortfolio {
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
		dedupCache,
	)
}

func newSingleGeneratorScenarioPortfolio(
	ctx *SolveContext,
	jobBudget *jobSearchBudget,
	availableGenerator framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
	dedupCache *scenarioDedupCache,
) *scenarioPortfolio {
	return newScenarioPortfolioForAvailableGenerators(
		ctx, jobBudget, []framework.ScenarioGeneratorRegistration{availableGenerator}, generatorBudget, dedupCache,
	)
}

func newScenarioPortfolioForAvailableGenerators(
	ctx *SolveContext,
	jobBudget *jobSearchBudget,
	availableGenerators []framework.ScenarioGeneratorRegistration,
	generatorBudget *generatorSearchBudget,
	dedupCache *scenarioDedupCache,
) *scenarioPortfolio {
	portfolio := &scenarioPortfolio{
		ctx:           ctx,
		jobBudget:     jobBudget,
		currentBudget: generatorBudget,
		stopReason:    SearchResultGeneratorsExhausted,
		dedupCache:    dedupCache,
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
			p.moveToNextGenerator()
			continue
		}

		generatorName := generator.Name()
		attemptStartedAt := time.Now()
		sn := generator.Next()
		if sn == nil {
			p.observeGeneratorAttempt(generatorName, string(SearchResultGeneratorsExhausted), attemptStartedAt)
			p.moveToNextGenerator()
			continue
		}
		byNodeScenario, ok := sn.(*scenario.ByNodeScenario)
		if !ok {
			p.observeGeneratorAttempt(generatorName, "unsupported", attemptStartedAt)
			log.InfraLogger.V(4).Infof(
				"Scenario generator <%s> returned unsupported scenario type %T",
				generatorName, sn,
			)
			p.moveToNextGenerator()
			continue
		}
		if byNodeScenario == nil {
			// Generators may signal exhaustion with a typed-nil scenario.
			p.observeGeneratorAttempt(generatorName, string(SearchResultGeneratorsExhausted), attemptStartedAt)
			p.moveToNextGenerator()
			continue
		}
		p.currentFingerprintSet = false
		if p.dedupCache != nil {
			// Fingerprint at emission time: generators may return a shared
			// scenario object that is mutated by later accumulation steps.
			fingerprint := fingerprintScenario(byNodeScenario)
			if p.dedupCache.isDuplicate(fingerprint) {
				metrics.IncScenarioSearchScenario(p.ctx.ActionType, generatorName, scenarioStateDuplicate)
				p.observeGeneratorAttempt(generatorName, scenarioStateDuplicate, attemptStartedAt)
				continue
			}
			p.currentFingerprint = fingerprint
			p.currentFingerprintSet = true
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

// MarkCurrentScenarioFailed records the last emitted scenario's fingerprint so
// equivalent candidates are skipped for the rest of this job's search. Only
// failed simulations may be recorded: a solved scenario must remain
// re-emittable because the final probe rebuilds the winning statement from
// scratch.
func (p *scenarioPortfolio) MarkCurrentScenarioFailed() {
	if p == nil || !p.currentFingerprintSet {
		return
	}
	p.dedupCache.recordFailed(p.currentFingerprint)
	p.currentFingerprintSet = false
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
	p.currentName = ""
	p.currentStartedAt = time.Time{}
	p.currentFingerprintSet = false
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
