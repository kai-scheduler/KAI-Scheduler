// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"math"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func nodeIdleOrReleasingGpuCapacity(ni *node_info.NodeInfo) float64 {
	idle, _ := ni.GetSumOfIdleGPUs()
	releasing, _ := ni.GetSumOfReleasingGPUs()
	return idle + releasing
}

// shiftElementLeft moves the element at currentPos to newPos by shifting the intervening
// elements one step to the right. Requires newPos <= currentPos; the slice is modified in place.
func shiftElementLeft[K any](slice []K, currentPos, newPos int) {
	if newPos >= currentPos {
		return
	}
	elem := slice[currentPos]
	for k := currentPos; k > newPos; k-- {
		slice[k] = slice[k-1]
	}
	slice[newPos] = elem
}

// greedyMatchRequirements checks whether each resource requirement (of numerical type) can be satisfied by one of the
// capacity holders using greedy virtual allocation. Both requirements and holders must be sorted
// descending. Returns true if all non-zero requirements can be matched.
func greedyMatchRequirements[K comparable](
	requirements []float64,
	holders []K,
	capacity func(K) float64,
) bool {
	if len(requirements) == 0 || requirements[0] == 0 {
		return true
	}
	if len(holders) >= len(requirements) &&
		capacity(holders[len(requirements)-1]) >= requirements[0] {
		return true
	}

	totals := make([]float64, len(holders))
	for i, holder := range holders {
		totals[i] = capacity(holder)
	}
	allocated := make([]float64, len(holders))
	tree := newMaxSegmentTree(totals)

	for _, required := range requirements {
		if required == 0 {
			return true
		}
		index, found := tree.firstAtLeast(required)
		if !found {
			return false
		}
		allocated[index] += required
		tree.update(index, totals[index]-allocated[index])
	}
	return true
}

// maxSegmentTree locates the first value in a fixed-order slice that meets a
// requirement while maintaining each range's maximum remaining capacity.
type maxSegmentTree struct {
	base   int
	values []float64
}

func newMaxSegmentTree(values []float64) maxSegmentTree {
	base := 1
	for base < len(values) {
		base *= 2
	}
	tree := maxSegmentTree{
		base:   base,
		values: make([]float64, 2*base),
	}
	for i := range tree.values {
		tree.values[i] = math.Inf(-1)
	}
	copy(tree.values[base:], values)
	for i := base - 1; i > 0; i-- {
		tree.values[i] = max(tree.values[2*i], tree.values[2*i+1])
	}
	return tree
}

func (tree maxSegmentTree) firstAtLeast(required float64) (int, bool) {
	if !resource_info.LessOrEqualWithTolerance(required, tree.values[1]) {
		return 0, false
	}
	index := 1
	for index < tree.base {
		left := 2 * index
		if resource_info.LessOrEqualWithTolerance(required, tree.values[left]) {
			index = left
		} else {
			index = left + 1
		}
	}
	return index - tree.base, true
}

func (tree maxSegmentTree) update(index int, value float64) {
	index += tree.base
	tree.values[index] = value
	for index /= 2; index > 0; index /= 2 {
		tree.values[index] = max(tree.values[2*index], tree.values[2*index+1])
	}
}

// iterateNewVictims calls fn for each victim task not yet in processedCache.
// Tasks with no assigned node are skipped. Each new task is added to processedCache before
// fn is called, preventing double-counting when the same victim appears across multiple calls.
// Returns the number of cache hits (tasks that were already processed).
func iterateNewVictims(
	victimTasks []*pod_info.PodInfo,
	processedCache map[common_info.PodID]bool,
	fn func(*pod_info.PodInfo),
) int {
	numCacheHits := 0
	for _, task := range victimTasks {
		if task.NodeName == "" {
			continue
		}
		if processedCache[task.UID] {
			numCacheHits++
			continue
		}
		processedCache[task.UID] = true
		fn(task)
	}
	return numCacheHits
}
