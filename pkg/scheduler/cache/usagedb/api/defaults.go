// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *UsageParams) SetDefaults() {
	if p.HalfLifePeriod == nil {
		// noop: disabled by default
	}
	if p.HalfLifePeriod != nil && p.HalfLifePeriod.Duration <= 0 {
		p.HalfLifePeriod = nil
	}
	if p.WindowSize == nil {
		p.WindowSize = &metav1.Duration{Duration: time.Hour * 24 * 7}
	}
	if p.WindowType == nil {
		windowType := SlidingWindow
		p.WindowType = &windowType
	}
	if p.FetchInterval == nil {
		p.FetchInterval = &metav1.Duration{Duration: 1 * time.Minute}
	}
	if p.StalenessPeriod == nil {
		p.StalenessPeriod = &metav1.Duration{Duration: 5 * time.Minute}
	}
	if p.WaitTimeout == nil {
		p.WaitTimeout = &metav1.Duration{Duration: 1 * time.Minute}
	}
}

// WindowType defines the type of time window for aggregating usage data
type WindowType string

const (
	// TumblingWindow represents non-overlapping, fixed-size time windows
	// Example: 1-hour windows at 0-1h, 1-2h, 2-3h
	TumblingWindow WindowType = "tumbling"

	// CronWindow represents a tumbling window that is defined by a cron string.
	// In this configuration, the window size is not used, and the window is defined by the cron string.
	// Example: every 1 hour at 00:00:00, every 1 hour at 01:00:00, etc.
	CronWindow WindowType = "cron"

	// SlidingWindow represents overlapping time windows that slide forward
	// Example: a 1-hour sliding window will consider the usage of the last 1 hour prior to the current time.
	SlidingWindow WindowType = "sliding"
)

// IsValid returns true if the WindowType is a valid value
func (wt WindowType) IsValid() bool {
	switch wt {
	case TumblingWindow, SlidingWindow, CronWindow:
		return true
	default:
		return false
	}
}

func (p *UsageParams) GetExtraDurationParamOrDefault(key string, defaultValue time.Duration) time.Duration {
	if p.ExtraParams == nil {
		return defaultValue
	}

	value, exists := p.ExtraParams[key]
	if !exists {
		return defaultValue
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}

	return duration
}

func (p *UsageParams) GetExtraStringParamOrDefault(key string, defaultValue string) string {
	if p.ExtraParams == nil {
		return defaultValue
	}

	value, exists := p.ExtraParams[key]
	if !exists {
		return defaultValue
	}

	return value
}
