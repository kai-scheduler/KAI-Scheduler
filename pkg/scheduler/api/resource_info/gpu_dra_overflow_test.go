// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"math"
	"testing"
)

// GetDraGpusCount sums the per-device-class DRA counts. With two GPU device
// classes each already at the int64 ceiling (reachable via a pod that holds one
// claim per class), the sum must saturate rather than wrap negative and poison
// the GPU quota returned by GetGpusQuota.
func TestGetDraGpusCount_saturatesOnOverflow(t *testing.T) {
	g := NewGpuResourceRequirement()
	g.SetDraGpus(map[string]int64{
		"gpu.nvidia.com": math.MaxInt64,
		"gpu.amd.com":    math.MaxInt64,
	})

	if got := g.GetDraGpusCount(); got != math.MaxInt64 {
		t.Fatalf("GetDraGpusCount() = %d, want %d (saturated, non-negative)", got, int64(math.MaxInt64))
	}
	if q := g.GetGpusQuota(); q < 0 {
		t.Fatalf("GetGpusQuota() = %f, want non-negative", q)
	}
}

// Add aggregates draGpuCounts across requirements (e.g. summing many pods into a
// queue total). Two requirements whose per-class counts sum past the int64
// ceiling must saturate rather than wrap negative.
func TestGpuResourceRequirementAdd_draGpuCountsSaturate(t *testing.T) {
	a := NewGpuResourceRequirement()
	a.SetDraGpus(map[string]int64{"gpu.nvidia.com": math.MaxInt64})
	b := NewGpuResourceRequirement()
	b.SetDraGpus(map[string]int64{"gpu.nvidia.com": math.MaxInt64})

	if err := a.Add(b); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if got := a.DraGpuCounts()["gpu.nvidia.com"]; got != math.MaxInt64 {
		t.Fatalf("draGpuCounts[gpu.nvidia.com] after Add = %d, want %d (saturated)", got, int64(math.MaxInt64))
	}
}
