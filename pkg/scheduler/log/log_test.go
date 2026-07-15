// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestSchedulerLoggerDoesNotExposeIsVerbose(t *testing.T) {
	loggerType := reflect.TypeOf((*SchedulerLogger)(nil)).Elem()
	_, found := loggerType.MethodByName("IsVerbose")
	require.False(t, found)
}

func TestVerboseLoggerDoRunsOnlyWhenEnabled(t *testing.T) {
	logger := newSchedulerLogger(3, zap.NewNop().Sugar())
	enabledCalls := 0
	disabledCalls := 0

	logger.V(3).Do(func() {
		enabledCalls++
	})
	logger.V(4).Do(func() {
		disabledCalls++
	})

	require.Equal(t, 1, enabledCalls)
	require.Zero(t, disabledCalls)
}

func TestNewBaseLoggerJSONOutput(t *testing.T) {
	var buf bytes.Buffer

	baseLogger, err := newBaseLogger(true, zapcore.AddSync(&buf))
	require.NoError(t, err)

	logger := newSchedulerLogger(3, baseLogger)
	logger.SetSessionID("session-1")
	logger.SetAction("allocate")
	logger.V(1).Info("hello world")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	require.Equal(t, "hello world", payload["msg"])
	require.Equal(t, "session-1", payload[sessionIDField])
	require.Equal(t, "allocate", payload[actionField])
	require.Equal(t, "INFO", payload["level"])
}

func TestNewBaseLoggerConsoleOutput(t *testing.T) {
	var buf bytes.Buffer

	baseLogger, err := newBaseLogger(false, zapcore.AddSync(&buf))
	require.NoError(t, err)

	logger := newSchedulerLogger(3, baseLogger)
	logger.SetSessionID("session-1")
	logger.SetAction("allocate")
	logger.V(1).Info("hello world")

	output := buf.String()
	require.Contains(t, output, "hello world")
	require.Contains(t, output, "\x1b[3")
	require.Contains(t, output, "allocate")
	require.False(t, strings.HasPrefix(strings.TrimSpace(output), "{"))
}
