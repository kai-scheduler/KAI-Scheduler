// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package math

import (
	stdmath "math"
	"testing"
)

func TestSaturatingAddInt64(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
	}{
		{"normal sum", 2, 3, 5},
		{"positive overflow saturates to MaxInt64", stdmath.MaxInt64, stdmath.MaxInt64, stdmath.MaxInt64},
		{"positive overflow by one", stdmath.MaxInt64, 1, stdmath.MaxInt64},
		{"negative overflow saturates to MinInt64", stdmath.MinInt64, stdmath.MinInt64, stdmath.MinInt64},
		{"mixed signs do not overflow", stdmath.MaxInt64, stdmath.MinInt64, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SaturatingAddInt64(tc.a, tc.b); got != tc.want {
				t.Fatalf("SaturatingAddInt64(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
