/*
Copyright 2023 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPluginArguments_GetFloat64(t *testing.T) {
	tests := []struct {
		name          string
		args          PluginArguments
		key           string
		defaultValue  float64
		expectedValue float64
		expectError   bool
		description   string
	}{
		{
			name:          "key exists with valid float",
			args:          PluginArguments{"multiplier": "2.5"},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 2.5,
			expectError:   false,
			description:   "should parse valid float value",
		},
		{
			name:          "key does not exist",
			args:          PluginArguments{},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 1.0,
			expectError:   false,
			description:   "should return default value and no error when key doesn't exist",
		},
		{
			name:          "invalid float format",
			args:          PluginArguments{"multiplier": "not-a-number"},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 1.0,
			expectError:   true,
			description:   "should return default value and error for invalid float format",
		},
		{
			name:          "zero value",
			args:          PluginArguments{"multiplier": "0"},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 0.0,
			expectError:   false,
			description:   "should parse zero correctly",
		},
		{
			name:          "negative value",
			args:          PluginArguments{"multiplier": "-1.5"},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: -1.5,
			expectError:   false,
			description:   "should parse negative values correctly",
		},
		{
			name:          "scientific notation",
			args:          PluginArguments{"multiplier": "1.5e2"},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 150.0,
			expectError:   false,
			description:   "should parse scientific notation",
		},
		{
			name:          "empty string value",
			args:          PluginArguments{"multiplier": ""},
			key:           "multiplier",
			defaultValue:  1.0,
			expectedValue: 1.0,
			expectError:   true,
			description:   "should return default value and error for empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.args.GetFloat64(tt.key, tt.defaultValue)
			assert.Equal(t, tt.expectedValue, result, tt.description)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestPluginArguments_GetInt(t *testing.T) {
	tests := []struct {
		name          string
		args          PluginArguments
		key           string
		defaultValue  int
		expectedValue int
		expectError   bool
		description   string
	}{
		{
			name:          "key exists with valid int",
			args:          PluginArguments{"count": "42"},
			key:           "count",
			defaultValue:  10,
			expectedValue: 42,
			expectError:   false,
			description:   "should parse valid int value",
		},
		{
			name:          "key does not exist",
			args:          PluginArguments{},
			key:           "count",
			defaultValue:  10,
			expectedValue: 10,
			expectError:   false,
			description:   "should return default value and no error when key doesn't exist",
		},
		{
			name:          "invalid int format",
			args:          PluginArguments{"count": "not-a-number"},
			key:           "count",
			defaultValue:  10,
			expectedValue: 10,
			expectError:   true,
			description:   "should return default value and error for invalid int format",
		},
		{
			name:          "float value for int",
			args:          PluginArguments{"count": "42.5"},
			key:           "count",
			defaultValue:  10,
			expectedValue: 10,
			expectError:   true,
			description:   "should return default value and error for non-integer numeric value",
		},
		{
			name:          "zero value",
			args:          PluginArguments{"count": "0"},
			key:           "count",
			defaultValue:  10,
			expectedValue: 0,
			expectError:   false,
			description:   "should parse zero correctly",
		},
		{
			name:          "negative value",
			args:          PluginArguments{"count": "-42"},
			key:           "count",
			defaultValue:  10,
			expectedValue: -42,
			expectError:   false,
			description:   "should parse negative values correctly",
		},
		{
			name:          "empty string value",
			args:          PluginArguments{"count": ""},
			key:           "count",
			defaultValue:  10,
			expectedValue: 10,
			expectError:   true,
			description:   "should return default value and error for empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.args.GetInt(tt.key, tt.defaultValue)
			assert.Equal(t, tt.expectedValue, result, tt.description)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestPluginArguments_GetBool(t *testing.T) {
	tests := []struct {
		name          string
		args          PluginArguments
		key           string
		defaultValue  bool
		expectedValue bool
		expectError   bool
		description   string
	}{
		{
			name:          "key exists with true",
			args:          PluginArguments{"enabled": "true"},
			key:           "enabled",
			defaultValue:  false,
			expectedValue: true,
			expectError:   false,
			description:   "should parse 'true' correctly",
		},
		{
			name:          "key exists with false",
			args:          PluginArguments{"enabled": "false"},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: false,
			expectError:   false,
			description:   "should parse 'false' correctly",
		},
		{
			name:          "key exists with 1",
			args:          PluginArguments{"enabled": "1"},
			key:           "enabled",
			defaultValue:  false,
			expectedValue: true,
			expectError:   false,
			description:   "should parse '1' as true",
		},
		{
			name:          "key exists with 0",
			args:          PluginArguments{"enabled": "0"},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: false,
			expectError:   false,
			description:   "should parse '0' as false",
		},
		{
			name:          "key does not exist",
			args:          PluginArguments{},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: true,
			expectError:   false,
			description:   "should return default value and no error when key doesn't exist",
		},
		{
			name:          "invalid bool format",
			args:          PluginArguments{"enabled": "not-a-bool"},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: true,
			expectError:   true,
			description:   "should return default value and error for invalid bool format",
		},
		{
			name:          "empty string value",
			args:          PluginArguments{"enabled": ""},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: true,
			expectError:   true,
			description:   "should return default value and error for empty string",
		},
		{
			name:          "uppercase TRUE",
			args:          PluginArguments{"enabled": "TRUE"},
			key:           "enabled",
			defaultValue:  false,
			expectedValue: true,
			expectError:   false,
			description:   "should parse 'TRUE' correctly (case insensitive)",
		},
		{
			name:          "uppercase FALSE",
			args:          PluginArguments{"enabled": "FALSE"},
			key:           "enabled",
			defaultValue:  true,
			expectedValue: false,
			expectError:   false,
			description:   "should parse 'FALSE' correctly (case insensitive)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.args.GetBool(tt.key, tt.defaultValue)
			assert.Equal(t, tt.expectedValue, result, tt.description)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestPluginArguments_GetString(t *testing.T) {
	tests := []struct {
		name          string
		args          PluginArguments
		key           string
		defaultValue  string
		expectedValue string
		description   string
	}{
		{
			name:          "key exists with non-empty value",
			args:          PluginArguments{"mode": "advanced"},
			key:           "mode",
			defaultValue:  "basic",
			expectedValue: "advanced",
			description:   "should return existing value",
		},
		{
			name:          "key does not exist",
			args:          PluginArguments{},
			key:           "mode",
			defaultValue:  "basic",
			expectedValue: "basic",
			description:   "should return default value when key doesn't exist",
		},
		{
			name:          "key exists with empty string",
			args:          PluginArguments{"mode": ""},
			key:           "mode",
			defaultValue:  "basic",
			expectedValue: "",
			description:   "should return empty string if that's the actual value",
		},
		{
			name:          "key exists with spaces",
			args:          PluginArguments{"mode": "  advanced  "},
			key:           "mode",
			defaultValue:  "basic",
			expectedValue: "  advanced  ",
			description:   "should preserve spaces in value",
		},
		{
			name:          "key exists with special characters",
			args:          PluginArguments{"mode": "advanced-mode_v1.0"},
			key:           "mode",
			defaultValue:  "basic",
			expectedValue: "advanced-mode_v1.0",
			description:   "should preserve special characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.args.GetString(tt.key, tt.defaultValue)
			assert.Equal(t, tt.expectedValue, result, tt.description)
		})
	}
}

// TestPluginArguments_RealWorldExample tests a realistic scenario similar to the proportion plugin
func TestPluginArguments_RealWorldExample(t *testing.T) {
	t.Run("proportion plugin scenario", func(t *testing.T) {
		args := PluginArguments{
			"relcaimerSaturationMultiplier": "2.5",
		}

		multiplier, err := args.GetFloat64("relcaimerSaturationMultiplier", 1.0)
		assert.NoError(t, err, "should parse without error")
		assert.Equal(t, 2.5, multiplier, "should parse valid multiplier")

		// Caller can perform custom validation
		if multiplier < 1.0 {
			t.Errorf("multiplier must be >= 1.0, got %v", multiplier)
		}
	})

	t.Run("proportion plugin scenario with validation", func(t *testing.T) {
		args := PluginArguments{
			"relcaimerSaturationMultiplier": "0.5",
		}

		multiplier, err := args.GetFloat64("relcaimerSaturationMultiplier", 1.0)
		assert.NoError(t, err, "should parse without error")
		assert.Equal(t, 0.5, multiplier, "should parse the value")

		// Caller performs validation and uses default if validation fails
		if multiplier < 1.0 {
			multiplier = 1.0 // Use default value
		}
		assert.Equal(t, 1.0, multiplier, "should use default value when validation fails")
	})

	t.Run("proportion plugin scenario with missing key", func(t *testing.T) {
		args := PluginArguments{}

		multiplier, err := args.GetFloat64("relcaimerSaturationMultiplier", 1.0)
		assert.NoError(t, err, "should not return error for missing key")
		assert.Equal(t, 1.0, multiplier, "should return default value when key doesn't exist")
	})
}
