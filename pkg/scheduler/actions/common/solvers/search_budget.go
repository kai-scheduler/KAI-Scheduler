// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package solvers

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
	deadline        deadlineBudget
}

type deadlineBudget struct {
	deadline  time.Time
	unlimited bool
	now       func() time.Time
}

type jobSearchBudget struct {
	deadline        deadlineBudget
	reducedBudget   bool
	generatorLimits map[string]time.Duration
}

type generatorSearchBudget struct {
	deadline deadlineBudget
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
		"maxJobSearchDuration", budgetFieldValue(budgets, func(b *kaiv1.ScenarioSearchBudgets) *metav1.Duration {
			return b.MaxJobSearchDuration
		}), constants.DefaultJobBudget,
	)
	if err != nil {
		return nil, err
	}
	minJobSearch, err := parseDurationWithDefault(
		"minJobSearchDuration", budgetFieldValue(budgets, func(b *kaiv1.ScenarioSearchBudgets) *metav1.Duration {
			return b.MinJobSearchDuration
		}), constants.DefaultMinJobBudget,
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
		deadline:        newDeadlineBudget(searchDurationForLimit(actionLimit), now),
	}, nil
}

func (b *ActionSearchBudget) BeginJob() *jobSearchBudget {
	if b == nil {
		return &jobSearchBudget{
			deadline: newDeadlineBudget(0, time.Now),
		}
	}
	now := b.clock()

	actionRemaining := b.Remaining()
	remaining := actionRemaining
	if b.jobLimit != 0 && b.jobLimit < remaining {
		remaining = b.jobLimit
	}
	if b.minJobSearch > remaining {
		remaining = b.minJobSearch
	}

	return &jobSearchBudget{
		deadline:        newDeadlineBudget(remaining, now),
		reducedBudget:   b.jobLimit > 0 && actionRemaining < b.jobLimit,
		generatorLimits: b.generatorLimits,
	}
}

func (b *ActionSearchBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	return b.deadline.Remaining()
}

func (b *ActionSearchBudget) Exhausted() bool {
	return b.Remaining() <= 0
}

func (b *jobSearchBudget) BeginGenerator(name string) *generatorSearchBudget {
	if b == nil {
		return &generatorSearchBudget{
			deadline: newDeadlineBudget(0, time.Now),
		}
	}
	now := b.clock()

	jobRemaining := b.Remaining()
	generatorRemaining := jobRemaining
	generatorLimit := b.generatorLimit(name)
	if generatorLimit != 0 && generatorLimit < generatorRemaining {
		generatorRemaining = generatorLimit
	}

	return &generatorSearchBudget{
		deadline: newDeadlineBudget(generatorRemaining, now),
	}
}

func (b *jobSearchBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	return b.deadline.Remaining()
}

func (b *jobSearchBudget) Exhausted() bool {
	return b.Remaining() <= 0
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
	return b.deadline.Remaining()
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
	return b.generatorLimits[constants.ActionDefault]
}

func newDeadlineBudget(remaining time.Duration, now func() time.Time) deadlineBudget {
	now = clockOrDefault(now)
	if remaining == unlimitedRemaining {
		return deadlineBudget{
			unlimited: true,
			now:       now,
		}
	}
	if remaining < 0 {
		remaining = 0
	}
	return deadlineBudget{
		deadline: now().Add(remaining),
		now:      now,
	}
}

func (b deadlineBudget) Remaining() time.Duration {
	if b.now == nil {
		return 0
	}
	if b.unlimited {
		return unlimitedRemaining
	}
	remaining := b.deadline.Sub(b.now())
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (b *ActionSearchBudget) clock() func() time.Time {
	if b == nil {
		return time.Now
	}
	return clockOrDefault(b.deadline.now)
}

func (b *jobSearchBudget) clock() func() time.Time {
	if b == nil {
		return time.Now
	}
	return clockOrDefault(b.deadline.now)
}

func searchDurationForLimit(limit time.Duration) time.Duration {
	if limit == 0 {
		return unlimitedRemaining
	}
	return limit
}

func clockOrDefault(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

func scenarioSearchBudgets(ssn *framework.Session) *kaiv1.ScenarioSearchBudgets {
	if ssn == nil || ssn.Config == nil {
		return nil
	}
	return ssn.Config.ScenarioSearchBudgets
}

func budgetFieldValue(
	budgets *kaiv1.ScenarioSearchBudgets, valueFn func(*kaiv1.ScenarioSearchBudgets) *metav1.Duration,
) *metav1.Duration {
	if budgets == nil {
		return nil
	}
	return valueFn(budgets)
}

func parseActionLimit(budgets *kaiv1.ScenarioSearchBudgets, action framework.ActionType) (time.Duration, error) {
	configuredLimits, err := parseDurationMap(
		"maxActionSearchDuration", actionLimitValues(budgets),
	)
	if err != nil {
		return 0, err
	}

	actionKey := scenarioSearchActionKey(action)
	if limit, found := configuredLimits[actionKey]; found {
		return limit, nil
	}
	if limit, found := configuredLimits[constants.ActionDefault]; found {
		return limit, nil
	}
	return mustParseDuration(defaultActionLimit()), nil
}

func parseGeneratorLimits(budgets *kaiv1.ScenarioSearchBudgets) (map[string]time.Duration, error) {
	configuredLimits, err := parseDurationMap(
		"maxGeneratorSearchDuration", generatorLimitValues(budgets),
	)
	if err != nil {
		return nil, err
	}

	defaultLimit, hasConfiguredDefault := configuredLimits[constants.ActionDefault]
	if !hasConfiguredDefault {
		defaultLimit = mustParseDuration(constants.DefaultGeneratorBudget)
	}

	generatorLimits := map[string]time.Duration{
		constants.ActionDefault: defaultLimit,
	}
	for name, limit := range configuredLimits {
		if name != constants.ActionDefault {
			generatorLimits[name] = limit
		}
	}
	setKnownGeneratorLimit(
		generatorLimits, configuredLimits, constants.GeneratorNodeLocalGreedy,
		constants.DefaultNodeLocalGreedy, defaultLimit, hasConfiguredDefault,
	)
	setKnownGeneratorLimit(
		generatorLimits, configuredLimits, constants.GeneratorMultiNodeGang,
		constants.DefaultMultiNodeGang, defaultLimit, hasConfiguredDefault,
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

func actionLimitValues(budgets *kaiv1.ScenarioSearchBudgets) map[string]metav1.Duration {
	if budgets == nil {
		return nil
	}
	return budgets.MaxActionSearchDuration
}

func generatorLimitValues(budgets *kaiv1.ScenarioSearchBudgets) map[string]metav1.Duration {
	if budgets == nil {
		return nil
	}
	return budgets.MaxGeneratorSearchDuration
}

func parseDurationWithDefault(fieldName string, value *metav1.Duration, defaultValue string) (time.Duration, error) {
	if value == nil {
		return validateDuration(fieldName, mustParseDuration(defaultValue))
	}
	return validateDuration(fieldName, value.Duration)
}

func parseDurationMap(fieldName string, durationValues map[string]metav1.Duration) (map[string]time.Duration, error) {
	durations := map[string]time.Duration{}
	for key, durationValue := range durationValues {
		duration, err := validateDuration(fmt.Sprintf("%s[%q]", fieldName, key), durationValue.Duration)
		if err != nil {
			return nil, err
		}
		durations[key] = duration
	}
	return durations, nil
}

func validateDuration(fieldName string, duration time.Duration) (time.Duration, error) {
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
		return constants.ActionReclaim
	case framework.Preempt:
		return constants.ActionPreempt
	case framework.Consolidation:
		return constants.ActionConsolidation
	default:
		return constants.ActionDefault
	}
}

func defaultActionLimit() string {
	return constants.DefaultActionBudget
}
