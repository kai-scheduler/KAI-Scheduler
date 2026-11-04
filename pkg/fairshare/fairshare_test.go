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

func TestDivideHierarchy(t *testing.T) {
	a1 := queue("a1", 1, 4, 1, math.Inf(1))
	a2 := queue("a2", 1, 0, 3, math.Inf(1))
	a := queue("a", 1, 0, 1, math.Inf(1))
	a.Children = []*Queue{a1, a2}
	b := queue("b", 1, 0, 1, math.Inf(1))

	DivideHierarchy(40, 0, []*Queue{a, b})

	for _, c := range []struct {
		q        *Queue
		expected float64
	}{{a, 20}, {b, 20}, {a1, 8}, {a2, 12}} {
		if c.q.FairShare != c.expected {
			t.Errorf("%s: fair share %v, expected %v", c.q.ID, c.q.FairShare, c.expected)
		}
	}
}
