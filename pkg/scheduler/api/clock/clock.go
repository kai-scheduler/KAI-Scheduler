// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package clock is the scheduler's decision clock.
//
// It is process-wide. Tests that call Set must not run in parallel with other
// tests that read Now or Since.
package clock

import (
	"sync/atomic"
	"time"
)

// seed is a decision clock that starts at opened and then follows real time.
type seed struct {
	opened time.Time
	start  time.Time
}

var current atomic.Pointer[seed]

// Now returns the decision time. With no seed, that is time.Now.
func Now() time.Time {
	if s := current.Load(); s != nil {
		return s.opened.Add(time.Since(s.start))
	}
	return time.Now()
}

// Since returns how long has passed since t on the decision clock.
func Since(t time.Time) time.Duration {
	return Now().Sub(t)
}

// Set starts the decision clock at t. Later Now calls are t plus the real
// time elapsed since Set. A zero t uses the wall clock. The returned function
// restores the previous clock.
func Set(t time.Time) (restore func()) {
	var next *seed
	if !t.IsZero() {
		next = &seed{opened: t, start: time.Now()}
	}
	prev := current.Swap(next)
	return func() {
		current.Store(prev)
	}
}

// Reset returns the decision clock to time.Now.
func Reset() {
	current.Store(nil)
}
