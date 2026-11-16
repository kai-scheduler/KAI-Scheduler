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
}

// RequestableShare is the part of the request the queue may get: its request capped by MaxAllowed.
func (q *Queue) RequestableShare() float64 {
	if q.MaxAllowed == Unlimited {
		return q.Request
	}
	return math.Min(q.MaxAllowed, q.Request)
}

// Divide divides total between queues and returns what is left. Queues keep their existing FairShare and receive
// their share on top of it.
func Divide(total, kValue float64, queues []*Queue) (remaining float64) {
	remaining = setDeservedResource(total, queues)
	if remaining <= 0 {
		return 0
	}
	return DivideOverQuota(remaining, kValue, queues)
}

func setDeservedResource(total float64, queues []*Queue) (remaining float64) {
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

type remainingRequestedResource struct {
	queue           *Queue
	remainingAmount float64
}

// DivideOverQuota divides total between queues by priority, highest first, and by weight within a priority, without
// a deserved phase. Fractions left by rounding go one unit at a time, highest priority first.
func DivideOverQuota(total, kValue float64, queues []*Queue) (remaining float64) {
	byPriority, priorities := getQueuesByPriority(queues)

	remaining = total
	for _, p := range priorities {
		if remaining <= 0 {
			break
		}
		var remainingRequested map[string]*remainingRequestedResource
		remaining, remainingRequested = divideUpToFairShare(remaining, kValue, byPriority[p])
		if remaining <= 0 || len(remainingRequested) == 0 {
			continue
		}
		remaining = divideRemainingResource(remaining, remainingRequested)
	}
	return remaining
}

func getQueuesByPriority(queues []*Queue) (map[int][]*Queue, []int) {
	queuesByPriority := map[int][]*Queue{}
	for _, q := range queues {
		queuesByPriority[q.Priority] = append(queuesByPriority[q.Priority], q)
	}
	priorities := make([]int, 0, len(queuesByPriority))
	for priority := range queuesByPriority {
		priorities = append(priorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	return queuesByPriority, priorities
}

func divideUpToFairShare(total, kValue float64, queues []*Queue) (float64, map[string]*remainingRequestedResource) {
	remainingRequested := map[string]*remainingRequestedResource{}
	for {
		anotherRound := false
		amountThisRound := total

		weights, weightsSum := calcShareWeights(queues, kValue)
		if weightsSum == 0 {
			break
		}
		for _, q := range queues {
			if total == 0 {
				break
			}
			if isQueueSatisfied(q) || q.OverQuotaWeight == 0 {
				continue
			}
			requested := getRemainingRequested(q)
			// The conversion stops arm64 from fusing this with the subtraction in getResourceToGiveInCurrentRound,
			// which would round differently from the scheduler.
			fairShare := float64(amountThisRound * (weights[q.ID] / weightsSum))
			give := getResourceToGiveInCurrentRound(fairShare, requested, q, remainingRequested)
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
	return total, remainingRequested
}

func calcShareWeights(queues []*Queue, kValue float64) (map[string]float64, float64) {
	totalWeights := 0.0
	for _, q := range queues {
		if getRemainingRequested(q) > 0 {
			totalWeights += q.OverQuotaWeight
		}
	}
	if totalWeights == 0 {
		return map[string]float64{}, 0
	}
	weights := map[string]float64{}
	sum := 0.0
	for _, q := range queues {
		if isQueueSatisfied(q) {
			continue
		}
		normalized := q.OverQuotaWeight / totalWeights
		weight := math.Max(0, normalized+kValue*(normalized-q.Usage))
		weights[q.ID] = weight
		sum += weight
	}
	return weights, sum
}

// getResourceToGiveInCurrentRound hands out whole units when the queue asks for more than its share, keeping the
// fraction for the leftover round.
func getResourceToGiveInCurrentRound(fairShare, requested float64, q *Queue,
	remainingRequested map[string]*remainingRequestedResource) float64 {
	if requested <= fairShare {
		delete(remainingRequested, q.ID)
		return requested
	}
	give := 0.0
	if rounded := math.Floor(fairShare); rounded > 0 {
		give = rounded
	}
	if fairShare-give > 0 {
		remainingRequested[q.ID] = &remainingRequestedResource{queue: q, remainingAmount: fairShare - give}
	}
	return give
}

// divideRemainingResource gives at most one unit to each queue, the largest leftover first, then the oldest queue,
// then by ID, so the result doesn't depend on map order.
func divideRemainingResource(total float64, remainingRequested map[string]*remainingRequestedResource) float64 {
	sorted := make([]*remainingRequestedResource, 0, len(remainingRequested))
	for _, l := range remainingRequested {
		sorted = append(sorted, l)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.remainingAmount != b.remainingAmount {
			return a.remainingAmount > b.remainingAmount
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

func isQueueSatisfied(q *Queue) bool {
	if q.Request <= q.FairShare {
		return true
	}
	return q.MaxAllowed != Unlimited && q.MaxAllowed <= q.FairShare
}

func getRemainingRequested(q *Queue) float64 {
	requested := q.RequestableShare()
	if requested < q.FairShare {
		return 0
	}
	return requested - q.FairShare
}
