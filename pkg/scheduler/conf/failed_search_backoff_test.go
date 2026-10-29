// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/api/constants"
)

func TestFailedSearchBackoff(t *testing.T) {
	tests := []struct {
		name             string
		minValues        map[string]string
		maxValues        map[string]string
		action           string
		wantMin, wantMax time.Duration
		wantErr          bool
	}{
		{name: "no backoff configured", action: constants.ActionReclaim},
		{
			name:      "the default applies to every action",
			minValues: map[string]string{constants.ActionDefault: "10s"},
			maxValues: map[string]string{constants.ActionDefault: "1m"},
			action:    constants.ActionPreempt,
			wantMin:   10 * time.Second, wantMax: time.Minute,
		},
		{
			name:      "an action's own setting wins over the default",
			minValues: map[string]string{constants.ActionDefault: "10s", constants.ActionReclaim: "20s"},
			maxValues: map[string]string{constants.ActionDefault: "1m", constants.ActionReclaim: "2m"},
			action:    constants.ActionReclaim,
			wantMin:   20 * time.Second, wantMax: 2 * time.Minute,
		},
		{
			name:      "zero disables it for one action",
			minValues: map[string]string{constants.ActionDefault: "10s", constants.ActionConsolidation: "0s"},
			action:    constants.ActionConsolidation,
		},
		{
			name:      "the maximum defaults to the minimum",
			minValues: map[string]string{constants.ActionReclaim: "10s"},
			action:    constants.ActionReclaim,
			wantMin:   10 * time.Second, wantMax: 10 * time.Second,
		},
		{
			name:      "a maximum below the minimum is raised to it",
			minValues: map[string]string{constants.ActionReclaim: "1m"},
			maxValues: map[string]string{constants.ActionDefault: "10s"},
			action:    constants.ActionReclaim,
			wantMin:   time.Minute, wantMax: time.Minute,
		},
		{
			name:      "an action without a setting or default has none",
			minValues: map[string]string{constants.ActionReclaim: "10s"},
			action:    constants.ActionPreempt,
		},
		{
			name:      "a negative minimum is invalid",
			minValues: map[string]string{constants.ActionReclaim: "-1s"},
			action:    constants.ActionReclaim,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minBackoff, maxBackoff, err := FailedSearchBackoff(tt.minValues, tt.maxValues, tt.action)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMin, minBackoff)
			require.Equal(t, tt.wantMax, maxBackoff)
		})
	}
}

func TestValidateFailedSearchBackoff(t *testing.T) {
	tests := []struct {
		name      string
		minValues map[string]string
		maxValues map[string]string
		errorText []string
	}{
		{
			name:      "valid, with a per-action override",
			minValues: map[string]string{constants.ActionDefault: "10s", constants.ActionPreempt: "0s"},
			maxValues: map[string]string{constants.ActionDefault: "2m"},
		},
		{
			name:      "an unknown action",
			minValues: map[string]string{"allocate": "10s"},
			errorText: []string{MinFailedSearchBackoffFlag, "allocate"},
		},
		{
			name:      "an unknown action in the maximum",
			maxValues: map[string]string{"allocate": "10s"},
			errorText: []string{MaxFailedSearchBackoffFlag, "allocate"},
		},
		{
			name:      "a negative backoff",
			minValues: map[string]string{constants.ActionReclaim: "-1s"},
			errorText: []string{MinFailedSearchBackoffFlag, constants.ActionReclaim},
		},
		{
			name:      "not a duration",
			minValues: map[string]string{constants.ActionReclaim: "soon"},
			errorText: []string{MinFailedSearchBackoffFlag, constants.ActionReclaim},
		},
		{
			name:      "a maximum from the default below an action's minimum",
			minValues: map[string]string{constants.ActionReclaim: "1m"},
			maxValues: map[string]string{constants.ActionDefault: "30s"},
			errorText: []string{MaxFailedSearchBackoffFlag, constants.ActionReclaim},
		},
		{
			name:      "a maximum without a minimum leaves the backoff off",
			maxValues: map[string]string{constants.ActionDefault: "30s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFailedSearchBackoff(tt.minValues, tt.maxValues)
			if tt.errorText == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, text := range tt.errorText {
				require.Contains(t, err.Error(), text)
			}
		})
	}
}
