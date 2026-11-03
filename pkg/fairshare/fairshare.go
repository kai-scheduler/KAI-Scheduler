// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package fairshare divides one resource between queues the way the scheduler's proportion plugin does: deserved
// quota first, then the over-quota remainder by priority and weight. It has no dependencies, so tools that need to
// predict the scheduler's fair share (simulators, admission or UI services) can use the scheduler's own code.
package fairshare

import (
	"math"
	"sort"
	"time"
)

// Unlimited marks an unlimited Deserved or MaxAllowed.
const Unlimited = float64(-1)

// Queue holds one queue's quota for a single resource. Divide adds the queue's share to FairShare.
type Queue struct {
	ID                string
	Priority          int
	CreationTimestamp time.Time

	Deserved        float64
	MaxAllowed      float64
	OverQuotaWeight float64
	Request         float64
	// Usage is the queue's historical usage, normalized to the cluster's capacity. It only matters when kValue > 0.
	Usage     float64
	FairShare float64

	Children []*Queue
}

// RequestableShare is the part of the request the queue may get: its request capped by MaxAllowed.
func (q *Queue) RequestableShare() float64 {
	if q.MaxAllowed == Unlimited {
		return q.Request
	}
	return math.Min(q.MaxAllowed, q.Request)
}

// DivideHierarchy divides total between the root queues and then each queue's fair share between its children.
func DivideHierarchy(total, kValue float64, roots []*Queue) {
	if len(roots) == 0 {
		return
	}
	Divide(total, kValue, roots)
	for _, q := range roots {
		DivideHierarchy(q.FairShare, kValue, q.Children)
	}
}

// Divide divides total between queues and returns what is left. Queues keep their existing FairShare and receive
// their share on top of it.
func Divide(total, kValue float64, queues []*Queue) (remaining float64) {
	remaining = setDeserved(total, queues)
	if remaining <= 0 {
		return 0
	}
	return DivideOverQuota(remaining, kValue, queues)
}

func setDeserved(total float64, queues []*Queue) (remaining float64) {
	remaining = total
	for _, q := range queues {
		deserved := q.Deserved
		if deserved == Unlimited {
			deserved = total
		}
		amount := math.Min(deserved, q.RequestableShare())
		q.FairShare += amount
		remaining -= amount
	}
	return remaining
}

type leftover struct {
	queue  *Queue
	amount float64
}

// DivideOverQuota divides total between queues by priority, highest first, and by weight within a priority, without
// a deserved phase. Fractions left by rounding go one unit at a time, highest priority first.
func DivideOverQuota(total, kValue float64, queues []*Queue) (remaining float64) {
	byPriority := map[int][]*Queue{}
	for _, q := range queues {
		byPriority[q.Priority] = append(byPriority[q.Priority], q)
	}
	priorities := make([]int, 0, len(byPriority))
	for p := range byPriority {
		priorities = append(priorities, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))

	remaining = total
	for _, p := range priorities {
		if remaining <= 0 {
			break
		}
		var left map[string]*leftover
		remaining, left = divideUpToFairShare(remaining, kValue, byPriority[p])
		if remaining <= 0 || len(left) == 0 {
			continue
		}
		remaining = divideLeftovers(remaining, left)
	}
	return remaining
}

func divideUpToFairShare(total, kValue float64, queues []*Queue) (float64, map[string]*leftover) {
	left := map[string]*leftover{}
	for {
		anotherRound := false
		amountThisRound := total

		weights, weightsSum := shareWeights(queues, kValue)
		if weightsSum == 0 {
			break
		}
		for _, q := range queues {
			if total == 0 {
				break
			}
			if isSatisfied(q) || q.OverQuotaWeight == 0 {
				continue
			}
			requested := remainingRequested(q)
			fairShare := amountThisRound * weights[q.ID] / weightsSum
			give := amountToGive(fairShare, requested, q, left)
			if give == 0 {
				continue
			}
			q.FairShare += give
			total -= give
			// A queue that asked for less than its share leaves some unassigned; another round gives it to the rest.
			anotherRound = anotherRound || requested < fairShare
		}
		if !anotherRound || total == 0 {
			break
		}
	}
	return total, left
}

func shareWeights(queues []*Queue, kValue float64) (map[string]float64, float64) {
	totalWeights := 0.0
	for _, q := range queues {
		if remainingRequested(q) > 0 {
			totalWeights += q.OverQuotaWeight
		}
	}
	if totalWeights == 0 {
		return map[string]float64{}, 0
	}
	weights := map[string]float64{}
	sum := 0.0
	for _, q := range queues {
		if isSatisfied(q) {
			continue
		}
		normalized := q.OverQuotaWeight / totalWeights
		weight := math.Max(0, normalized+kValue*(normalized-q.Usage))
		weights[q.ID] = weight
		sum += weight
	}
	return weights, sum
}

// amountToGive hands out whole units when the queue asks for more than its share, keeping the fraction for the
// leftover round.
func amountToGive(fairShare, requested float64, q *Queue, left map[string]*leftover) float64 {
	if requested <= fairShare {
		delete(left, q.ID)
		return requested
	}
	give := 0.0
	if rounded := math.Floor(fairShare); rounded > 0 {
		give = rounded
	}
	if fairShare-give > 0 {
		left[q.ID] = &leftover{queue: q, amount: fairShare - give}
	}
	return give
}

// divideLeftovers gives at most one unit to each queue, the largest leftover first, then the oldest queue, then by
// ID, so the result doesn't depend on map order.
func divideLeftovers(total float64, left map[string]*leftover) float64 {
	sorted := make([]*leftover, 0, len(left))
	for _, l := range left {
		sorted = append(sorted, l)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.amount != b.amount {
			return a.amount > b.amount
		}
		if !a.queue.CreationTimestamp.Equal(b.queue.CreationTimestamp) {
			return a.queue.CreationTimestamp.Before(b.queue.CreationTimestamp)
		}
		return a.queue.ID < b.queue.ID
	})
	for _, l := range sorted {
		if total == 0 {
			break
		}
		give := math.Min(1, total)
		l.queue.FairShare += give
		total -= give
	}
	return total
}

func isSatisfied(q *Queue) bool {
	if q.Request <= q.FairShare {
		return true
	}
	return q.MaxAllowed != Unlimited && q.MaxAllowed <= q.FairShare
}

func remainingRequested(q *Queue) float64 {
	requested := q.RequestableShare()
	if requested < q.FairShare {
		return 0
	}
	return requested - q.FairShare
}
