// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fairshare

import (
	"math"
	"testing"
	"time"
)

func queue(id string, priority int, deserved, weight, request float64) *Queue {
	return &Queue{ID: id, Priority: priority, Deserved: deserved, MaxAllowed: Unlimited, OverQuotaWeight: weight, Request: request}
}

func TestDivide(t *testing.T) {
	for _, tc := range []struct {
		name     string
		total    float64
		queues   []*Queue
		expected map[string]float64
	}{
		{
			name:     "deserved first, then the rest by weight",
			total:    70,
			queues:   []*Queue{queue("a", 1, 10, 1, math.Inf(1)), queue("b", 1, 20, 3, math.Inf(1))},
			expected: map[string]float64{"a": 20, "b": 50},
		},
		{
			name:     "higher priority is served first",
			total:    40,
			queues:   []*Queue{queue("high", 2, 0, 1, 30), queue("low", 1, 0, 9, math.Inf(1))},
			expected: map[string]float64{"high": 30, "low": 10},
		},
		{
			name:  "a queue stops at its max allowed",
			total: 20,
			queues: []*Queue{
				{ID: "capped", Priority: 1, MaxAllowed: 5, OverQuotaWeight: 1, Request: math.Inf(1)},
				queue("other", 1, 0, 1, math.Inf(1)),
			},
			expected: map[string]float64{"capped": 5, "other": 15},
		},
		{
			name:  "rounding leftovers stay with the higher priority",
			total: 6,
			queues: []*Queue{
				queue("high-1", 2, 1, 1, 5),
				queue("high-2", 2, 1, 1, 5),
				queue("low", 1, 1, 1, 5),
			},
			expected: map[string]float64{"high-1": 3, "high-2": 2, "low": 1},
		},
		{
			name:  "the last unit goes to the oldest of equal queues",
			total: 5,
			queues: []*Queue{
				{ID: "newer", Priority: 1, Deserved: 2, MaxAllowed: Unlimited, OverQuotaWeight: 2, Request: 3, CreationTimestamp: time.Unix(2, 0)},
				{ID: "older", Priority: 1, Deserved: 2, MaxAllowed: Unlimited, OverQuotaWeight: 2, Request: 3, CreationTimestamp: time.Unix(1, 0)},
			},
			expected: map[string]float64{"older": 3, "newer": 2},
		},
		{
			name:     "deserved is given in full even when it exceeds the total",
			total:    10,
			queues:   []*Queue{queue("a", 1, 6, 1, math.Inf(1)), queue("b", 1, 6, 1, math.Inf(1))},
			expected: map[string]float64{"a": 6, "b": 6},
		},
		{
			name:  "unlimited deserved covers the whole request",
			total: 10,
			queues: []*Queue{
				queue("unlimited", 1, Unlimited, 1, 7),
				queue("other", 1, 0, 1, math.Inf(1)),
			},
			expected: map[string]float64{"unlimited": 7, "other": 3},
		},
		{
			name:  "queues after the total runs out get nothing",
			total: 10,
			queues: []*Queue{
				queue("a", 1, 0, 1, math.Inf(1)),
				queue("b", 1, 0, 1, math.Inf(1)),
				queue("zero-weight", 1, 0, 0, math.Inf(1)),
			},
			expected: map[string]float64{"a": 5, "b": 5, "zero-weight": 0},
		},
		{
			name:  "a single leftover unit goes to the oldest queue",
			total: 1,
			queues: []*Queue{
				{ID: "a", Priority: 1, MaxAllowed: Unlimited, OverQuotaWeight: 1, Request: math.Inf(1), CreationTimestamp: time.Unix(3, 0)},
				{ID: "b", Priority: 1, MaxAllowed: Unlimited, OverQuotaWeight: 1, Request: math.Inf(1), CreationTimestamp: time.Unix(1, 0)},
				{ID: "c", Priority: 1, MaxAllowed: Unlimited, OverQuotaWeight: 1, Request: math.Inf(1), CreationTimestamp: time.Unix(2, 0)},
			},
			expected: map[string]float64{"a": 0, "b": 1, "c": 0},
		},
		{
			name:     "a single leftover unit goes to the largest fraction",
			total:    1,
			queues:   []*Queue{queue("light", 1, 0, 1, math.Inf(1)), queue("heavy", 1, 0, 3, math.Inf(1))},
			expected: map[string]float64{"light": 0, "heavy": 1},
		},
		{
			name:  "a queue already above its request gets nothing more",
			total: 4,
			queues: []*Queue{
				{ID: "above", Priority: 1, MaxAllowed: Unlimited, OverQuotaWeight: 1, Request: 3, FairShare: 5},
				queue("other", 1, 0, 1, math.Inf(1)),
			},
			expected: map[string]float64{"above": 5, "other": 4},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Divide(tc.total, 0, tc.queues)
			for _, q := range tc.queues {
				if q.FairShare != tc.expected[q.ID] {
					t.Errorf("%s: fair share %v, expected %v", q.ID, q.FairShare, tc.expected[q.ID])
				}
			}
		})
	}
}

// The leftover round hands out whole units; a queue whose remaining request or MaxAllowed is smaller than a unit
// must not be given the whole unit.
func TestDivideOverQuotaRemainderCapsAtTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		total    float64
		queues   []*Queue
		expected map[string]float64
	}{
		{
			name:     "fractional requests",
			total:    10.2,
			queues:   []*Queue{queue("a", 1, 0, 1, 3.5), queue("b", 1, 0, 1, 3.5), queue("c", 1, 0, 1, 3.5)},
			expected: map[string]float64{"a": 3.5, "b": 3.5, "c": 3.2},
		},
		{
			name:  "fractional max allowed",
			total: 1.2,
			queues: []*Queue{
				{ID: "a", Priority: 1, MaxAllowed: 0.5, OverQuotaWeight: 1, Request: 5},
				queue("b", 1, 0, 1, 0.5), queue("c", 1, 0, 1, 0.5),
			},
			expected: map[string]float64{"a": 0.5, "b": 0.5, "c": 0.2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remaining := DivideOverQuota(tc.total, 0, tc.queues)
			if math.Abs(remaining) > 1e-9 {
				t.Errorf("remaining = %v, expected 0", remaining)
			}
			for _, q := range tc.queues {
				if math.Abs(q.FairShare-tc.expected[q.ID]) > 1e-9 {
					t.Errorf("queue %s fair share = %v, expected %v", q.ID, q.FairShare, tc.expected[q.ID])
				}
			}
		})
	}
}
