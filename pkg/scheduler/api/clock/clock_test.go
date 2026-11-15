// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"testing"
	"time"
)

func TestSetRunsForwardFromTheSeed(t *testing.T) {
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := Set(opened)
	defer restore()

	first := Now()
	if first.Before(opened) || first.Sub(opened) > time.Second {
		t.Fatalf("Now() = %s, want %s plus a short delay", first, opened)
	}
	time.Sleep(20 * time.Millisecond)
	second := Now()
	if second.Sub(first) < 10*time.Millisecond {
		t.Fatalf("clock did not advance: %s then %s", first, second)
	}
	if Since(opened) < 10*time.Millisecond {
		t.Fatalf("Since(opened) = %s", Since(opened))
	}
}

func TestSetRestoresThePreviousClock(t *testing.T) {
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := Set(opened)
	restore()
	if skew := time.Since(Now()); skew < 0 || skew > time.Second {
		t.Fatalf("restored clock skew %s", skew)
	}
}

func TestZeroSetAndResetUseTheWallClock(t *testing.T) {
	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := Set(opened)
	defer restore()

	back := Set(time.Time{})
	if skew := time.Since(Now()); skew < 0 || skew > time.Second {
		t.Fatalf("zero Set skew %s", skew)
	}
	back()
	if Now().Year() != 2024 {
		t.Fatal("restore did not return to the seed")
	}

	Reset()
	if skew := time.Since(Now()); skew < 0 || skew > time.Second {
		t.Fatalf("Reset skew %s", skew)
	}
}
