// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/scenariosearch"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

const unlimitedRemaining = time.Duration(1<<63 - 1)

// ActionSearchBudget tracks the top-level deadline for one scheduling action search.
type ActionSearchBudget struct {
	action          framework.ActionType
	actionLimit     time.Duration
	jobLimit        time.Duration
	minJobSearch    time.Duration
	generatorLimits map[string]time.Duration
	startedAt       time.Time
	now             func() time.Time
}

type jobSearchBudget struct {
	remainingAtStart time.Duration
	reducedBudget    bool
	startedAt        time.Time
	now              func() time.Time
	generatorLimits  map[string]time.Duration
}

type generatorSearchBudget struct {
	remainingAtStart time.Duration
	startedAt        time.Time
	now              func() time.Time
}

// NewActionSearchBudget parses scenario search budget config for one scheduler action.
func NewActionSearchBudget(ssn *framework.Session, action framework.ActionType) (*ActionSearchBudget, error) {
	return newActionSearchBudgetWithClock(ssn, action, time.Now)
}

func newActionSearchBudgetWithClock(
	ssn *framework.Session, action framework.ActionType, now func() time.Time,
) (*ActionSearchBudget, error) {
	now = clockOrDefault(now)

	budgets := scenarioSearchBudgets(ssn)
	actionLimit, err := parseActionLimit(budgets, action)
	if err != nil {
		return nil, err
	}
	jobLimit, err := parseDurationWithDefault(
		"maxJobSearchDuration", budgetFieldValue(budgets, func(b *conf.ScenarioSearchBudgets) string {
			return b.MaxJobSearchDuration
		}), scenariosearch.DefaultJobBudget,
	)
	if err != nil {
		return nil, err
	}
	minJobSearch, err := parseDurationWithDefault(
		"minJobSearchDuration", budgetFieldValue(budgets, func(b *conf.ScenarioSearchBudgets) string {
			return b.MinJobSearchDuration
		}), scenariosearch.DefaultMinJobBudget,
	)
	if err != nil {
		return nil, err
	}
	if jobLimit != 0 && minJobSearch >= jobLimit {
		return nil, fmt.Errorf("minJobSearchDuration must be less than maxJobSearchDuration")
	}
	generatorLimits, err := parseGeneratorLimits(budgets)
	if err != nil {
		return nil, err
	}
	metrics.SetScenarioSearchActionBudget(action, actionLimit)
	metrics.SetScenarioSearchJobBudget(jobLimit)
	for generator, limit := range generatorLimits {
		metrics.SetScenarioSearchGeneratorBudget(generator, limit)
	}

	return &ActionSearchBudget{
		action:          action,
		actionLimit:     actionLimit,
		jobLimit:        jobLimit,
		minJobSearch:    minJobSearch,
		generatorLimits: generatorLimits,
		startedAt:       now(),
		now:             now,
	}, nil
}

func (b *ActionSearchBudget) BeginJob() *jobSearchBudget {
	if b == nil {
		now := time.Now
		return &jobSearchBudget{
			startedAt: now(),
			now:       now,
		}
	}
	now := clockOrDefault(b.now)

	actionRemaining := b.Remaining()
	if actionRemaining <= 0 {
		return &jobSearchBudget{
			startedAt:       now(),
			now:             now,
			generatorLimits: b.generatorLimits,
		}
	}

	remaining := actionRemaining
	if b.jobLimit != 0 && b.jobLimit < remaining {
		remaining = b.jobLimit
	}

	return &jobSearchBudget{
		remainingAtStart: remaining,
		reducedBudget:    b.minJobSearch > 0 && actionRemaining < b.minJobSearch,
		startedAt:        now(),
		now:              now,
		generatorLimits:  b.generatorLimits,
	}
}

func (b *ActionSearchBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	if b.actionLimit == 0 {
		return unlimitedRemaining
	}
	return remainingFromStart(b.actionLimit, b.startedAt, b.now)
}

func (b *ActionSearchBudget) Exhausted() bool {
	return b.Remaining() <= 0
}

func (b *jobSearchBudget) BeginGenerator(name string) *generatorSearchBudget {
	if b == nil {
		now := time.Now
		return &generatorSearchBudget{
			startedAt: now(),
			now:       now,
		}
	}
	now := clockOrDefault(b.now)

	jobRemaining := b.Remaining()
	if jobRemaining <= 0 {
		return &generatorSearchBudget{
			startedAt: now(),
			now:       now,
		}
	}

	generatorRemaining := jobRemaining
	generatorLimit := b.generatorLimit(name)
	if generatorLimit != 0 && generatorLimit < generatorRemaining {
		generatorRemaining = generatorLimit
	}

	return &generatorSearchBudget{
		remainingAtStart: generatorRemaining,
		startedAt:        now(),
		now:              now,
	}
}

func (b *jobSearchBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	return remainingFromStart(b.remainingAtStart, b.startedAt, b.now)
}

func (b *jobSearchBudget) ReducedBudget() bool {
	if b == nil {
		return false
	}
	return b.reducedBudget
}

func (b *generatorSearchBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	return remainingFromStart(b.remainingAtStart, b.startedAt, b.now)
}

func (b *generatorSearchBudget) Exhausted() bool {
	return b.Remaining() <= 0
}

func (b *jobSearchBudget) generatorLimit(name string) time.Duration {
	if b == nil || b.generatorLimits == nil {
		return 0
	}
	if limit, found := b.generatorLimits[name]; found {
		return limit
	}
	return b.generatorLimits[scenariosearch.ActionDefault]
}

func remainingFromStart(limit time.Duration, startedAt time.Time, now func() time.Time) time.Duration {
	if limit <= 0 || now == nil {
		return 0
	}
	remaining := limit - now().Sub(startedAt)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func clockOrDefault(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

func scenarioSearchBudgets(ssn *framework.Session) *conf.ScenarioSearchBudgets {
	if ssn == nil || ssn.Config == nil {
		return nil
	}
	return ssn.Config.ScenarioSearchBudgets
}

func budgetFieldValue(
	budgets *conf.ScenarioSearchBudgets, valueFn func(*conf.ScenarioSearchBudgets) string,
) string {
	if budgets == nil {
		return ""
	}
	return valueFn(budgets)
}

func parseActionLimit(budgets *conf.ScenarioSearchBudgets, action framework.ActionType) (time.Duration, error) {
	configuredLimits, err := parseDurationMap(
		"maxActionSearchDuration", actionLimitStrings(budgets),
	)
	if err != nil {
		return 0, err
	}

	actionKey := scenarioSearchActionKey(action)
	if limit, found := configuredLimits[actionKey]; found {
		return limit, nil
	}
	if limit, found := configuredLimits[scenariosearch.ActionDefault]; found {
		return limit, nil
	}
	return mustParseDuration(defaultActionLimit()), nil
}

func parseGeneratorLimits(budgets *conf.ScenarioSearchBudgets) (map[string]time.Duration, error) {
	configuredLimits, err := parseDurationMap(
		"maxGeneratorSearchDuration", generatorLimitStrings(budgets),
	)
	if err != nil {
		return nil, err
	}

	defaultLimit, hasConfiguredDefault := configuredLimits[scenariosearch.ActionDefault]
	if !hasConfiguredDefault {
		defaultLimit = mustParseDuration(scenariosearch.DefaultGeneratorBudget)
	}

	generatorLimits := map[string]time.Duration{
		scenariosearch.ActionDefault: defaultLimit,
	}
	for name, limit := range configuredLimits {
		if name != scenariosearch.ActionDefault {
			generatorLimits[name] = limit
		}
	}
	setKnownGeneratorLimit(
		generatorLimits, configuredLimits, scenariosearch.GeneratorNodeLocalGreedy,
		scenariosearch.DefaultNodeLocalGreedy, defaultLimit, hasConfiguredDefault,
	)
	setKnownGeneratorLimit(
		generatorLimits, configuredLimits, scenariosearch.GeneratorMultiNodeGang,
		scenariosearch.DefaultMultiNodeGang, defaultLimit, hasConfiguredDefault,
	)
	return generatorLimits, nil
}

func setKnownGeneratorLimit(
	generatorLimits map[string]time.Duration,
	configuredLimits map[string]time.Duration,
	name string,
	defaultValue string,
	defaultLimit time.Duration,
	hasConfiguredDefault bool,
) {
	if _, found := configuredLimits[name]; found {
		return
	}
	if hasConfiguredDefault {
		generatorLimits[name] = defaultLimit
		return
	}
	generatorLimits[name] = mustParseDuration(defaultValue)
}

func actionLimitStrings(budgets *conf.ScenarioSearchBudgets) map[string]string {
	if budgets == nil {
		return nil
	}
	return budgets.MaxActionSearchDuration
}

func generatorLimitStrings(budgets *conf.ScenarioSearchBudgets) map[string]string {
	if budgets == nil {
		return nil
	}
	return budgets.MaxGeneratorSearchDuration
}

func parseDurationWithDefault(fieldName, value, defaultValue string) (time.Duration, error) {
	if value == "" {
		value = defaultValue
	}
	return parseDuration(fieldName, value)
}

func parseDurationMap(fieldName string, durationStrings map[string]string) (map[string]time.Duration, error) {
	durations := map[string]time.Duration{}
	for key, durationString := range durationStrings {
		if durationString == "" {
			continue
		}
		duration, err := parseDuration(fmt.Sprintf("%s[%q]", fieldName, key), durationString)
		if err != nil {
			return nil, err
		}
		durations[key] = duration
	}
	return durations, nil
}

func parseDuration(fieldName, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid Go duration: %w", fieldName, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("%s must be non-negative", fieldName)
	}
	return duration, nil
}

func mustParseDuration(value string) time.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		panic(err)
	}
	return duration
}

func scenarioSearchActionKey(action framework.ActionType) string {
	switch action {
	case framework.Reclaim:
		return scenariosearch.ActionReclaim
	case framework.Preempt:
		return scenariosearch.ActionPreempt
	case framework.Consolidation:
		return scenariosearch.ActionConsolidation
	default:
		return scenariosearch.ActionDefault
	}
}

func defaultActionLimit() string {
	return scenariosearch.DefaultActionBudget
}
