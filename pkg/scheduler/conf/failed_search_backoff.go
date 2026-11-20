// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/kai-scheduler/api/constants"
)

const (
	MinFailedSearchBackoffFlag = "min-failed-search-backoff"
	MaxFailedSearchBackoffFlag = "max-failed-search-backoff"
)

// failedSearchBackoffActions are the keys the failed-search backoff flags take.
var failedSearchBackoffActions = []string{
	constants.ActionDefault, constants.ActionReclaim, constants.ActionPreempt, constants.ActionConsolidation,
}

// FailedSearchBackoff returns the bounds of action's failed-search backoff from the flags' action=duration
// values, zero when it is off. An action without its own value uses the default one; the maximum
// defaults to, and is at least, the minimum.
func FailedSearchBackoff(minValues, maxValues map[string]string, action string) (time.Duration, time.Duration, error) {
	minBackoff, _, err := actionDuration(MinFailedSearchBackoffFlag, minValues, action)
	if err != nil || minBackoff == 0 {
		return 0, 0, err
	}
	maxBackoff, _, err := actionDuration(MaxFailedSearchBackoffFlag, maxValues, action)
	if err != nil {
		return 0, 0, err
	}
	return minBackoff, max(minBackoff, maxBackoff), nil
}

// ValidateFailedSearchBackoff checks the flags' values: known actions, non-negative durations, and no
// maximum below the minimum.
func ValidateFailedSearchBackoff(minValues, maxValues map[string]string) error {
	for _, flag := range []struct {
		name   string
		values map[string]string
	}{{MinFailedSearchBackoffFlag, minValues}, {MaxFailedSearchBackoffFlag, maxValues}} {
		for _, action := range slices.Sorted(maps.Keys(flag.values)) {
			if !slices.Contains(failedSearchBackoffActions, action) {
				return fmt.Errorf("--%s: unknown action %q, expected one of %v", flag.name, action, failedSearchBackoffActions)
			}
		}
	}
	for _, action := range failedSearchBackoffActions {
		minBackoff, minFound, err := actionDuration(MinFailedSearchBackoffFlag, minValues, action)
		if err != nil {
			return err
		}
		maxBackoff, maxFound, err := actionDuration(MaxFailedSearchBackoffFlag, maxValues, action)
		if err != nil {
			return err
		}
		if minFound && maxFound && maxBackoff < minBackoff {
			return fmt.Errorf("--%s for %q must not be less than --%s",
				MaxFailedSearchBackoffFlag, action, MinFailedSearchBackoffFlag)
		}
	}
	return nil
}

// actionDuration returns the duration the flag sets for action, or else its default one.
func actionDuration(flag string, values map[string]string, action string) (time.Duration, bool, error) {
	key := action
	value, found := values[key]
	if !found {
		key = constants.ActionDefault
		value, found = values[key]
	}
	if !found {
		return 0, false, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, fmt.Errorf("--%s[%q]: %w", flag, key, err)
	}
	if duration < 0 {
		return 0, false, fmt.Errorf("--%s[%q] must be non-negative", flag, key)
	}
	return duration, true, nil
}
