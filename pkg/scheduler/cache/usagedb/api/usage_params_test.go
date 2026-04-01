// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestUsageParams_SetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *UsageParams
		expected *UsageParams
	}{
		{
			name:  "empty params should set defaults",
			input: &UsageParams{},
			expected: &UsageParams{
				HalfLifePeriod: nil, // should remain nil (disabled by default)
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "negative half life period should be disabled",
			input: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: -3 * time.Minute},
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "zero half life period should be disabled",
			input: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 0 * time.Minute},
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "params with half life set should preserve it",
			input: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 30 * time.Minute},
			},
			expected: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 30 * time.Minute},
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "params with window size set should preserve it",
			input: &UsageParams{
				WindowSize: ptr.To(model.Duration(2 * time.Hour)),
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(2 * time.Hour)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "params with window type set should preserve it",
			input: &UsageParams{
				WindowType: &[]WindowType{TumblingWindow}[0],
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{TumblingWindow}[0],
			},
		},
		{
			name: "all params set should preserve all",
			input: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 45 * time.Minute},
				WindowSize:     ptr.To(model.Duration(3 * time.Hour)),
				WindowType:     &[]WindowType{TumblingWindow}[0],
			},
			expected: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 45 * time.Minute},
				WindowSize:     ptr.To(model.Duration(3 * time.Hour)),
				WindowType:     &[]WindowType{TumblingWindow}[0],
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.SetDefaults()

			if tt.expected.HalfLifePeriod == nil {
				assert.Nil(t, tt.input.HalfLifePeriod)
			} else {
				require.NotNil(t, tt.input.HalfLifePeriod)
				assert.Equal(t, tt.expected.HalfLifePeriod.Duration, tt.input.HalfLifePeriod.Duration)
			}

			require.NotNil(t, tt.input.WindowSize)
			assert.Equal(t, time.Duration(*tt.expected.WindowSize), time.Duration(*tt.input.WindowSize))

			require.NotNil(t, tt.input.WindowType)
			assert.Equal(t, *tt.expected.WindowType, *tt.input.WindowType)
		})
	}
}

func TestUsageParams_GetWindowTypeOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    *UsageParams
		expected WindowType
	}{
		{
			name:     "nil window type should return default",
			input:    &UsageParams{WindowType: nil},
			expected: SlidingWindow,
		},
		{
			name:     "sliding window type should return sliding",
			input:    &UsageParams{WindowType: &[]WindowType{SlidingWindow}[0]},
			expected: SlidingWindow,
		},
		{
			name:     "tumbling window type should return tumbling",
			input:    &UsageParams{WindowType: &[]WindowType{TumblingWindow}[0]},
			expected: TumblingWindow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.SetDefaults()
			assert.Equal(t, tt.expected, *tt.input.WindowType)
		})
	}
}

func TestUsageDBConfig_GetUsageParams(t *testing.T) {
	tests := []struct {
		name     string
		config   *UsageDBConfig
		expected *UsageParams
	}{
		{
			name: "nil usage params should return defaults",
			config: &UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://localhost:9090",
				UsageParams:      nil,
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "empty usage params should return defaults",
			config: &UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://localhost:9090",
				UsageParams:      &UsageParams{},
			},
			expected: &UsageParams{
				HalfLifePeriod: nil,
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "partial usage params should fill in defaults",
			config: &UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://localhost:9090",
				UsageParams: &UsageParams{
					HalfLifePeriod: &metav1.Duration{Duration: 30 * time.Minute},
				},
			},
			expected: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 30 * time.Minute},
				WindowSize:     ptr.To(model.Duration(time.Hour * 24 * 7)),
				WindowType:     &[]WindowType{SlidingWindow}[0],
			},
		},
		{
			name: "full usage params should be preserved",
			config: &UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://localhost:9090",
				UsageParams: &UsageParams{
					HalfLifePeriod: &metav1.Duration{Duration: 45 * time.Minute},
					WindowSize:     ptr.To(model.Duration(2 * time.Hour)),
					WindowType:     &[]WindowType{TumblingWindow}[0],
				},
			},
			expected: &UsageParams{
				HalfLifePeriod: &metav1.Duration{Duration: 45 * time.Minute},
				WindowSize:     ptr.To(model.Duration(2 * time.Hour)),
				WindowType:     &[]WindowType{TumblingWindow}[0],
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetUsageParams()
			require.NotNil(t, result)

			if tt.expected.HalfLifePeriod == nil {
				assert.Nil(t, result.HalfLifePeriod)
			} else {
				require.NotNil(t, result.HalfLifePeriod)
				assert.Equal(t, tt.expected.HalfLifePeriod.Duration, result.HalfLifePeriod.Duration)
			}

			require.NotNil(t, result.WindowSize)
			assert.Equal(t, time.Duration(*tt.expected.WindowSize), time.Duration(*result.WindowSize))

			require.NotNil(t, result.WindowType)
			assert.Equal(t, *tt.expected.WindowType, *result.WindowType)
		})
	}
}

func TestUsageDBConfig_GetUsageParams_ImmutableOriginal(t *testing.T) {
	// Test that GetUsageParams doesn't modify the original config
	originalParams := &UsageParams{
		HalfLifePeriod: &metav1.Duration{Duration: 30 * time.Minute},
	}

	config := &UsageDBConfig{
		ClientType:       "prometheus",
		ConnectionString: "http://localhost:9090",
		UsageParams:      originalParams,
	}

	result := config.GetUsageParams()

	// Modify the result
	result.WindowSize = ptr.To(model.Duration(5 * time.Hour))

	// Original should remain unchanged
	assert.Nil(t, originalParams.WindowSize)
	assert.Equal(t, 30*time.Minute, originalParams.HalfLifePeriod.Duration)
}

func TestWindowType_IsValid(t *testing.T) {
	tests := []struct {
		name       string
		windowType WindowType
		expected   bool
	}{
		{
			name:       "tumbling window is valid",
			windowType: TumblingWindow,
			expected:   true,
		},
		{
			name:       "sliding window is valid",
			windowType: SlidingWindow,
			expected:   true,
		},
		{
			name:       "empty string is invalid",
			windowType: WindowType(""),
			expected:   false,
		},
		{
			name:       "random string is invalid",
			windowType: WindowType("invalid"),
			expected:   false,
		},
		{
			name:       "mixed case tumbling is invalid",
			windowType: WindowType("Tumbling"),
			expected:   false,
		},
		{
			name:       "mixed case sliding is invalid",
			windowType: WindowType("Sliding"),
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.windowType.IsValid()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUsageParams_ZeroValues(t *testing.T) {
	// Test behavior with zero duration values
	params := &UsageParams{
		HalfLifePeriod: &metav1.Duration{Duration: time.Duration(0)},
		WindowSize:     ptr.To(model.Duration(time.Duration(0))),
	}

	params.SetDefaults()

	require.Nil(t, params.HalfLifePeriod)

	// Zero values should be preserved, not replaced with defaults
	require.NotNil(t, params.WindowSize)
	assert.Equal(t, time.Duration(0), time.Duration(*params.WindowSize))

	require.NotNil(t, params.WindowType)
	assert.Equal(t, SlidingWindow, *params.WindowType)
}

func TestWindowSize_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantErr  bool
		expected model.Duration
	}{
		{
			name:     "week format 1w",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"1w"}}`,
			expected: model.Duration(7 * 24 * time.Hour),
		},
		{
			name:     "day format 7d",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"7d"}}`,
			expected: model.Duration(7 * 24 * time.Hour),
		},
		{
			name:     "hour format 168h",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"168h"}}`,
			expected: model.Duration(168 * time.Hour),
		},
		{
			name:     "minute format 30m",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"30m"}}`,
			expected: model.Duration(30 * time.Minute),
		},
		{
			name:     "second format 60s",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"60s"}}`,
			expected: model.Duration(60 * time.Second),
		},
		{
			name:     "multiple weeks 2w",
			json:     `{"clientType":"prometheus","usageParams":{"windowSize":"2w"}}`,
			expected: model.Duration(14 * 24 * time.Hour),
		},
		{
			name:    "invalid format should fail",
			json:    `{"clientType":"prometheus","usageParams":{"windowSize":"invalid"}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &UsageDBConfig{}
			err := json.Unmarshal([]byte(tt.json), config)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, config.UsageParams.WindowSize)
			assert.Equal(t, tt.expected, *config.UsageParams.WindowSize)
		})
	}
}
