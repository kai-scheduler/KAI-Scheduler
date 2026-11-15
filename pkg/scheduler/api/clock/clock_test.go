// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"testing"
	"time"
)

func TestSetRestoresThePreviousClock(t *testing.T) {
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := Set(func() time.Time { return opened })
	if !Now().Equal(opened) {
		t.Fatalf("Now() = %s, want %s", Now(), opened)
	}
	if got := Since(opened.Add(-time.Second)); got != time.Second {
		t.Fatalf("Since() = %s, want 1s", got)
	}
	restore()
	if skew := time.Since(Now()); skew < 0 || skew > time.Second {
		t.Fatalf("restored clock skew %s", skew)
	}
}

func TestNilSetUsesTheWallClock(t *testing.T) {
	restore := Set(func() time.Time { return time.Time{} })
	defer restore()
	again := Set(nil)
	defer again()
	if skew := time.Since(Now()); skew < 0 || skew > time.Second {
		t.Fatalf("nil Set skew %s", skew)
	}
}
