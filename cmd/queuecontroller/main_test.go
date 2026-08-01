// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"testing"
)

func TestNewLoggerUsesJSONWhenZapDevelFalse(t *testing.T) {
	fs := flag.NewFlagSet("queuecontroller", flag.ContinueOnError)
	logOptions := bindLoggerFlags(fs)

	if err := fs.Parse([]string{"--zap-devel=false"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	buffer := &bytes.Buffer{}
	logger := newLogger(logOptions, buffer)
	logger.Info("queue controller started", "component", "queue-controller")

	var logLine map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buffer.Bytes()), &logLine); err != nil {
		t.Fatalf("expected JSON log line, got %q: %v", buffer.String(), err)
	}

	if got := logLine["msg"]; got != "queue controller started" {
		t.Fatalf("unexpected message: %v", got)
	}

	if got := logLine["component"]; got != "queue-controller" {
		t.Fatalf("unexpected component: %v", got)
	}
}

func TestBindLoggerFlagsDefaultsToDevelopmentMode(t *testing.T) {
	fs := flag.NewFlagSet("queuecontroller", flag.ContinueOnError)
	logOptions := bindLoggerFlags(fs)

	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	if !logOptions.Development {
		t.Fatal("expected development mode by default")
	}

	if logOptions.TimeEncoder == nil {
		t.Fatal("expected time encoder to be configured")
	}
}
