// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package memorylimit

import (
	"context"
	"math"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoMemLimitRatio(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		valid    bool
		expected float64
	}{
		{name: "default", valid: true, expected: 0.9},
		{name: "valid", value: "0.85", valid: true, expected: 0.85},
		{name: "zero", value: "0", valid: false},
		{name: "negative", value: "-0.1", valid: false},
		{name: "greater than one", value: "1.1", valid: false},
		{name: "not a number", value: "NaN", valid: false},
		{name: "infinite", value: "Inf", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(RatioEnv, "")
			if err := os.Unsetenv(RatioEnv); err != nil {
				t.Fatal(err)
			}
			if tt.value != "" {
				t.Setenv(RatioEnv, tt.value)
			}

			ratio, err := resolveRatio(0.9)
			if !tt.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ratio)
		})
	}
}

func TestCalculateGoMemLimit(t *testing.T) {
	assert.Equal(t, int64(5*1024*1024*1024), calculate(7*1024*1024*1024, 5.0/7.0))
	assert.Equal(t, int64(math.MaxInt64), calculate(math.MaxUint64, 1))
	assert.Equal(t, int64(math.MaxInt64), calculate(math.MaxUint64, 0.85))
}

func TestMemoryLimitManagerUpdate(t *testing.T) {
	var (
		providerLimit uint64 = 7 * 1024 * 1024 * 1024
		providerErr   error
		setLimits     []int64
		logMessages   []string
	)
	manager := &manager{
		ratio: 0.9,
		provider: func() (uint64, error) {
			return providerLimit, providerErr
		},
		setter: func(limit int64) int64 {
			setLimits = append(setLimits, limit)
			return 0
		},
		logInfo: func(format string, args ...interface{}) {
			logMessages = append(logMessages, format)
		},
		logWarning: func(format string, args ...interface{}) {
			logMessages = append(logMessages, format)
		},
	}

	manager.update()
	manager.update()
	providerLimit = 8 * 1024 * 1024 * 1024
	manager.update()
	providerLimit = 6 * 1024 * 1024 * 1024
	manager.update()
	providerErr = memlimit.ErrNoLimit
	manager.update()
	manager.update()
	providerErr = nil
	manager.update()

	require.Equal(t, []int64{
		calculate(7*1024*1024*1024, 0.9),
		calculate(8*1024*1024*1024, 0.9),
		calculate(6*1024*1024*1024, 0.9),
	}, setLimits)
	assert.Len(t, logMessages, 5)
	assert.Contains(t, logMessages[3], "unlimited")
	assert.Contains(t, logMessages[4], "recovered")
}

func TestMemoryLimitManagerStopsWithContext(t *testing.T) {
	manager := &manager{
		ratio:      0.9,
		provider:   func() (uint64, error) { return 1024, nil },
		setter:     func(int64) int64 { return 0 },
		logInfo:    func(string, ...interface{}) {},
		logWarning: func(string, ...interface{}) {},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := manager.start(ctx)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("memory limit manager did not stop")
	}
}

func TestStartMemoryLimitManagerHonorsExplicitGoMemLimit(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	require.NoError(t, Start(context.Background(), Config{DefaultRatio: 0.9}))
}

func TestStartMemoryLimitManagerRejectsInvalidRatio(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(RatioEnv, "0")
	err := Start(context.Background(), Config{DefaultRatio: 0.9})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), RatioEnv))
}

func TestControllerDefaultRatio(t *testing.T) {
	t.Setenv(RatioEnv, "")
	require.NoError(t, os.Unsetenv(RatioEnv))
	ratio, err := resolveRatio(0.85)
	require.NoError(t, err)
	assert.Equal(t, 0.85, ratio)
	assert.Equal(t, int64(3650722201), calculate(4<<30, ratio))
}

func TestEmptyGoMemLimitDoesNotSkipConfiguration(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv(RatioEnv, "invalid")
	require.Error(t, Start(context.Background(), Config{DefaultRatio: 0.85}))
}

func TestExplicitGoMemLimitPrecedesInvalidRatio(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	t.Setenv(RatioEnv, "invalid")
	before := debug.SetMemoryLimit(-1)
	require.NoError(t, Start(context.Background(), Config{DefaultRatio: 0.85}))
	assert.Equal(t, before, debug.SetMemoryLimit(-1))
}
