// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionNames(t *testing.T) {
	tests := []struct {
		name   string
		config *SchedulerConfiguration
		want   []string
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty",
			config: &SchedulerConfiguration{},
		},
		{
			name:   "blank only",
			config: &SchedulerConfiguration{Actions: "   "},
		},
		{
			name:   "commas only",
			config: &SchedulerConfiguration{Actions: ",,,"},
		},
		{
			name:   "spaces around names",
			config: &SchedulerConfiguration{Actions: " allocate , consolidation "},
			want:   []string{"allocate", "consolidation"},
		},
		{
			name:   "normal list",
			config: &SchedulerConfiguration{Actions: "allocate,consolidation,reclaim"},
			want:   []string{"allocate", "consolidation", "reclaim"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.config.ActionNames())
		})
	}
}
