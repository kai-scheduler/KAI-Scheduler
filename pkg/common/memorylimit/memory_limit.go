// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package memorylimit reserves headroom below the container memory limit.
package memorylimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
)

const (
	// RatioEnv allows tuning the headroom reserved for non-runtime memory.
	RatioEnv               = "KAI_GOMEMLIMIT_RATIO"
	defaultRefreshInterval = 15 * time.Second
)

// Config supplies the memory budget and logging for a controller.
type Config struct {
	DefaultRatio float64
	LogInfo      func(string, ...interface{})
	LogWarning   func(string, ...interface{})
}

type manager struct {
	ratio               float64
	provider            func() (uint64, error)
	setter              func(int64) int64
	logInfo, logWarning func(string, ...interface{})
	hasLimit            bool
	lastLimit           int64
	lastErrText         string
}

// Start applies a cgroup memory budget before controller caches are initialized.
func Start(ctx context.Context, config Config) error {
	info, warning := config.LogInfo, config.LogWarning
	if info == nil {
		info = func(string, ...interface{}) {}
	}
	if warning == nil {
		warning = func(string, ...interface{}) {}
	}
	if value := os.Getenv("GOMEMLIMIT"); value != "" {
		info("GOMEMLIMIT is explicitly set to %q; skipping automatic cgroup-derived memory limiting", value)
		return nil
	}
	ratio, err := resolveRatio(config.DefaultRatio)
	if err != nil {
		return err
	}
	m := &manager{ratio: ratio, provider: memlimit.FromCgroup, setter: debug.SetMemoryLimit, logInfo: info, logWarning: warning}
	m.start(ctx)
	return nil
}

func resolveRatio(defaultRatio float64) (float64, error) {
	ratio := defaultRatio
	if raw, ok := os.LookupEnv(RatioEnv); ok {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", RatioEnv, err)
		}
		ratio = parsed
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio > 1 {
		return 0, fmt.Errorf("%s must be in the range (0, 1], got %v", RatioEnv, ratio)
	}
	return ratio, nil
}

func (m *manager) start(ctx context.Context) <-chan struct{} {
	m.update()
	ticker := time.NewTicker(defaultRefreshInterval)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.update()
			}
		}
	}()
	return done
}

func (m *manager) update() {
	cgroupLimit, err := m.provider()
	if err != nil {
		m.logError(err)
		return
	}
	if m.lastErrText != "" {
		m.logInfo("cgroup memory limit detection recovered")
		m.lastErrText = ""
	}
	limit := calculate(cgroupLimit, m.ratio)
	if m.hasLimit && limit == m.lastLimit {
		return
	}
	previous := m.setter(limit)
	m.lastLimit = limit
	m.hasLimit = true
	m.logInfo("updated Go memory limit to %d bytes from cgroup limit %d bytes with ratio %.2f (previous %d bytes)", limit, cgroupLimit, m.ratio, previous)
}

func (m *manager) logError(err error) {
	if err.Error() == m.lastErrText {
		return
	}
	m.lastErrText = err.Error()
	if errors.Is(err, memlimit.ErrNoLimit) {
		m.logWarning("cgroup memory limit is unlimited; retaining current Go memory limit")
		return
	}
	m.logWarning("unable to detect cgroup memory limit; retaining current Go memory limit: %v", err)
}

func calculate(cgroupLimit uint64, ratio float64) int64 {
	limit := float64(cgroupLimit) * ratio
	if limit >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(limit)
}
