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

var nowFn atomic.Value // func() time.Time

func init() {
	nowFn.Store(time.Now)
}

// Now returns the decision time. Production uses time.Now.
func Now() time.Time {
	return nowFn.Load().(func() time.Time)()
}

// Since returns how long has passed since t on the decision clock.
func Since(t time.Time) time.Duration {
	return Now().Sub(t)
}

// Set installs fn until the returned function restores the previous clock.
// A nil fn restores time.Now.
func Set(fn func() time.Time) (restore func()) {
	if fn == nil {
		fn = time.Now
	}
	prev := nowFn.Swap(fn).(func() time.Time)
	return func() {
		nowFn.Store(prev)
	}
}
