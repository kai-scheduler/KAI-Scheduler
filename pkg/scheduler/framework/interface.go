/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"strconv"
	"time"

	"github.com/xhit/go-str2duration/v2"
)

type ActionType string

const (
	Reclaim           ActionType = "reclaim"
	Preempt           ActionType = "preempt"
	Allocate          ActionType = "allocate"
	Consolidation     ActionType = "consolidation"
	StaleGangEviction ActionType = "stalegangeviction"
)

// Action is the interface of scheduler action.
type Action interface {
	// The unique name of Action.
	Name() ActionType

	// Execute allocates the cluster's resources into each queue.
	Execute(ssn *Session)
}

type Plugin interface {
	// The unique name of Plugin.
	Name() string

	OnSessionOpen(ssn *Session)
	OnSessionClose(ssn *Session)
}

type PluginArguments map[string]string

// GetFloat64 parses a float64 value from plugin arguments.
// Returns the parsed value and nil error on success.
// Returns defaultValue and nil if the key doesn't exist.
// Returns defaultValue and an error if parsing fails.
func (pa PluginArguments) GetFloat64(key string, defaultValue float64) (float64, error) {
	val, exists := pa[key]
	if !exists {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse %q as float64: %w", val, err)
	}

	return parsed, nil
}

// GetInt parses an int value from plugin arguments.
// Returns the parsed value and nil error on success.
// Returns defaultValue and nil if the key doesn't exist.
// Returns defaultValue and an error if parsing fails.
func (pa PluginArguments) GetInt(key string, defaultValue int) (int, error) {
	val, exists := pa[key]
	if !exists {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse %q as int: %w", val, err)
	}

	return parsed, nil
}

// GetBool parses a bool value from plugin arguments.
// Returns the parsed value and nil error on success.
// Returns defaultValue and nil if the key doesn't exist.
// Returns defaultValue and an error if parsing fails.
func (pa PluginArguments) GetBool(key string, defaultValue bool) (bool, error) {
	val, exists := pa[key]
	if !exists {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse %q as bool: %w", val, err)
	}

	return parsed, nil
}

// GetString returns a string value from plugin arguments.
// If the key doesn't exist, returns defaultValue.
func (pa PluginArguments) GetString(key string, defaultValue string) string {
	if val, exists := pa[key]; exists {
		return val
	}
	return defaultValue
}

// GetDuration parses a time.Duration value from plugin arguments.
// Returns the parsed value and nil error on success.
// Returns defaultValue and nil if the key doesn't exist.
// Returns defaultValue and an error if parsing fails.
func (pa PluginArguments) GetDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	val, exists := pa[key]
	if !exists {
		return defaultValue, nil
	}

	parsed, err := str2duration.ParseDuration(val)
	if err != nil {
		return defaultValue, fmt.Errorf("failed to parse %q as duration: %w", val, err)
	}

	return parsed, nil
}
