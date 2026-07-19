// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package math holds dependency-light arithmetic helpers that the scheduler's
// resource model can use without pulling in Kubernetes or DRA client packages.
package math

import stdmath "math"

// SaturatingAdd returns a + b, clamped to [math.MinInt64, math.MaxInt64]
// on overflow instead of wrapping around to the opposite sign.
func SaturatingAdd(a, b int64) int64 {
	sum := a + b
	// Overflow can only happen when both operands share a sign and the sign of
	// the result flips.
	if a > 0 && b > 0 && sum < 0 {
		return stdmath.MaxInt64
	}
	if a < 0 && b < 0 && sum >= 0 {
		return stdmath.MinInt64
	}
	return sum
}
