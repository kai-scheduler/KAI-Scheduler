// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

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

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	goMemLimitRatioEnv        = "KAI_GOMEMLIMIT_RATIO"
	defaultGoMemLimitRatio    = 0.9
	goMemLimitRefreshInterval = 15 * time.Second
)

type memoryLimitProvider func() (uint64, error)
type memoryLimitSetter func(int64) int64
type memoryLimitLogger func(string, ...interface{})

type memoryLimitManager struct {
	ratio       float64
	provider    memoryLimitProvider
	setter      memoryLimitSetter
	logInfo     memoryLimitLogger
	logWarning  memoryLimitLogger
	hasLimit    bool
	lastLimit   int64
	lastErrText string
}

func startMemoryLimitManager(ctx context.Context) error {
	if value, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		log.InfraLogger.V(2).Infof("GOMEMLIMIT is explicitly set to %q; skipping automatic cgroup-derived memory limiting", value)
		return nil
	}

	ratio, err := goMemLimitRatio()
	if err != nil {
		return err
	}

	manager := &memoryLimitManager{
		ratio:      ratio,
		provider:   memlimit.FromCgroup,
		setter:     debug.SetMemoryLimit,
		logInfo:    log.InfraLogger.V(2).Infof,
		logWarning: log.InfraLogger.Warningf,
	}
	manager.start(ctx)
	return nil
}

func goMemLimitRatio() (float64, error) {
	ratio := defaultGoMemLimitRatio
	if raw, ok := os.LookupEnv(goMemLimitRatioEnv); ok {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", goMemLimitRatioEnv, err)
		}
		ratio = parsed
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio > 1 {
		return 0, fmt.Errorf("%s must be in the range (0, 1], got %v", goMemLimitRatioEnv, ratio)
	}
	return ratio, nil
}

func (m *memoryLimitManager) start(ctx context.Context) <-chan struct{} {
	m.update()

	ticker := time.NewTicker(goMemLimitRefreshInterval)
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

func (m *memoryLimitManager) update() {
	cgroupLimit, err := m.provider()
	if err != nil {
		m.logError(err)
		return
	}
	if m.lastErrText != "" {
		m.logInfo("cgroup memory limit detection recovered")
		m.lastErrText = ""
	}

	goLimit := calculateGoMemLimit(cgroupLimit, m.ratio)
	if m.hasLimit && goLimit == m.lastLimit {
		return
	}

	previous := m.setter(goLimit)
	m.lastLimit = goLimit
	m.hasLimit = true
	m.logInfo("updated Go memory limit to %d bytes from cgroup limit %d bytes with ratio %.2f (previous %d bytes)",
		goLimit, cgroupLimit, m.ratio, previous)
}

func (m *memoryLimitManager) logError(err error) {
	errText := err.Error()
	if errText == m.lastErrText {
		return
	}
	m.lastErrText = errText
	if errors.Is(err, memlimit.ErrNoLimit) {
		m.logWarning("cgroup memory limit is unlimited; retaining current Go memory limit")
		return
	}
	m.logWarning("unable to detect cgroup memory limit; retaining current Go memory limit: %v", err)
}

func calculateGoMemLimit(cgroupLimit uint64, ratio float64) int64 {
	limit := uint64(float64(cgroupLimit) * ratio)
	if limit > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(limit)
}
