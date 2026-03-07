// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package minruntime

import (
	"fmt"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resolver struct {
	preemptMinRuntimeCache   map[common_info.QueueID]metav1.Duration
	reclaimMinRuntimeCache   map[common_info.QueueID]map[common_info.QueueID]metav1.Duration
	defaultPreemptMinRuntime metav1.Duration
	defaultReclaimMinRuntime metav1.Duration

	queues map[common_info.QueueID]*queue_info.QueueInfo
}

func NewResolver(queues map[common_info.QueueID]*queue_info.QueueInfo, defaultPreemptMinRuntime metav1.Duration, defaultReclaimMinRuntime metav1.Duration) *resolver {
	return &resolver{
		queues:                   queues,
		defaultPreemptMinRuntime: defaultPreemptMinRuntime,
		defaultReclaimMinRuntime: defaultReclaimMinRuntime,
		preemptMinRuntimeCache:   make(map[common_info.QueueID]metav1.Duration),
		reclaimMinRuntimeCache:   make(map[common_info.QueueID]map[common_info.QueueID]metav1.Duration),
	}
}

func (r *resolver) getPreemptMinRuntime(
	queue *queue_info.QueueInfo,
) (metav1.Duration, error) {
	if queue == nil {
		return r.defaultPreemptMinRuntime, fmt.Errorf("queue is nil")
	}

	if minRuntime, ok := r.preemptMinRuntimeCache[queue.UID]; ok {
		return minRuntime, nil
	}
	return r.resolvePreemptMinRuntime(queue)
}

// resolvePreemptMinRuntime resolves min-runtime for preemptions
// Starting from the leaf-queue, walk the tree until the first defined preempt-min-runtime is set and use that
func (r *resolver) resolvePreemptMinRuntime(
	queue *queue_info.QueueInfo,
) (metav1.Duration, error) {
	// Start with the provided leaf queue
	currentQueue := queue

	minRuntime := r.defaultPreemptMinRuntime
	// Walk up the tree until we find a set preempt-min-runtime or reach the root
	for currentQueue != nil {
		if currentQueue.PreemptMinRuntime != nil {
			minRuntime = *currentQueue.PreemptMinRuntime
			break
		}
		currentQueue = r.queues[currentQueue.ParentQueue]
	}

	// If no preempt-min-runtime is set in the queue tree, use default
	r.cachePreemptMinRuntime(queue, minRuntime)
	return minRuntime, nil
}

// getReclaimMinRuntime returns min-runtime for reclaims
// Depending on the resolveMethod, it will use either:
// 1. queue: Starting from the leaf-queue, walk the tree (similar to preempt)
// 2. lca: Use the lowest common ancestor approach
func (r *resolver) getReclaimMinRuntime(
	resolveMethod string,
	preemptorQueue *queue_info.QueueInfo,
	preempteeQueue *queue_info.QueueInfo,
) (metav1.Duration, error) {
	if preemptorQueue == nil {
		return r.defaultReclaimMinRuntime, fmt.Errorf("preemptorQueue is nil")
	}
	if preempteeQueue == nil {
		return r.defaultReclaimMinRuntime, fmt.Errorf("preempteeQueue is nil")
	}

	if minRuntime, ok := r.reclaimMinRuntimeCache[preemptorQueue.UID][preempteeQueue.UID]; ok {
		return minRuntime, nil
	}
	if resolveMethod == resolveMethodLCA {
		return r.resolveReclaimMinRuntimeLCA(preemptorQueue, preempteeQueue)
	}
	return r.resolveReclaimMinRuntimeQueue(preemptorQueue, preempteeQueue)
}

func (r *resolver) resolveReclaimMinRuntimeQueue(
	preemptorQueue *queue_info.QueueInfo,
	preempteeQueue *queue_info.QueueInfo,
) (metav1.Duration, error) {
	// Start with the provided leaf queue
	currentQueue := preempteeQueue

	minRuntime := r.defaultReclaimMinRuntime

	// Walk up the tree until we find a set reclaim-min-runtime or reach the root
	for currentQueue != nil {
		if currentQueue.ReclaimMinRuntime != nil {
			minRuntime = *currentQueue.ReclaimMinRuntime
			break
		}
		currentQueue = r.queues[currentQueue.ParentQueue]
	}

	// If no reclaim-min-runtime is set in the queue tree, use default
	r.cacheReclaimMinRuntime(preemptorQueue, preempteeQueue, minRuntime)
	return minRuntime, nil
}

// resolveReclaimMinRuntimeLCA resolves min-runtime for reclaims using the LCA method
// 1. Resolve the lowest common ancestor (LCA) between the leaf-queues of preemptor and preemptee
// 2. Walk 1 step down to the child of the LCA that is an ancestor to the preemptee's leaf queue (or is the leaf queue)
// 3. Use the reclaim-min-runtime from this queue, if it is set. Otherwise move back up towards root of tree
//
// Note: Top-level queues (with empty parent) are treated as siblings with an implicit shadow parent
func (r *resolver) resolveReclaimMinRuntimeLCA(
	preemptorQueue *queue_info.QueueInfo,
	preempteeQueue *queue_info.QueueInfo,
) (metav1.Duration, error) {
	if preemptorQueue == nil || preempteeQueue == nil {
		return r.defaultReclaimMinRuntime, fmt.Errorf("preemptorQueue (%v) or preempteeQueue (%v) is nil", preemptorQueue, preempteeQueue)
	}

	// Get hierarchy paths for both queues
	preemptorPath := r.getQueueHierarchyPath(preemptorQueue)
	preempteePath := r.getQueueHierarchyPath(preempteeQueue)

	// Handle case where both queues are from different top-level hierarchies
	// Consider them as siblings with a shadow parent
	topLevelPreemptor := preemptorPath[0]
	topLevelPreemptee := preempteePath[0]

	// If both are top-level queues (empty parent) and different
	if topLevelPreemptor.UID != topLevelPreemptee.UID {
		// For different top-level queues:
		// The LCA is the implicit shadow parent at level -1 (doesn't exist in path)
		// So we'd want to look at the top-level queue (first in path) that's the ancestor
		// of the preemptee, which is preempteePath[0]

		// Check if the top-level preemptee queue has reclaim-min-runtime set
		duration := topLevelPreemptee.ReclaimMinRuntime
		if duration == nil {
			duration = &r.defaultReclaimMinRuntime
		}
		r.cacheReclaimMinRuntime(preemptorQueue, preempteeQueue, *duration)
		return *duration, nil
	}

	// Find the lowest common ancestor (LCA)
	lcaIndex := 0
	minLength := min(len(preemptorPath), len(preempteePath))

	for i := 0; i < minLength; i++ {
		if preemptorPath[i].UID != preempteePath[i].UID {
			break
		}
		lcaIndex = i
	}
	// If there's a path down from LCA to preemptee, take one step down to the child of the LCA
	if lcaIndex+1 < len(preempteePath) {
		lcaIndex++
	}

	duration := r.defaultReclaimMinRuntime

	// From the current LCA or child of LCA, move up towards root and select first available override
	for i := lcaIndex; i >= 0; i-- {
		if preempteePath[i].ReclaimMinRuntime != nil {
			duration = *preempteePath[i].ReclaimMinRuntime
			break
		}
	}

	// If no reclaim-min-runtime is found in the hierarchy, use default
	r.cacheReclaimMinRuntime(preemptorQueue, preempteeQueue, duration)
	return duration, nil
}

// getQueueHierarchyPath returns the queue hierarchy from top-level queue to the specified queue
// Note: For top-level queues (empty parent), the path will only contain the queue itself
func (r *resolver) getQueueHierarchyPath(
	queue *queue_info.QueueInfo,
) []*queue_info.QueueInfo {
	var hierarchyPath []*queue_info.QueueInfo

	// Start with the queue itself
	currentQueue := queue

	// Build the path from queue to parent
	for currentQueue != nil {
		// Add current queue to the path (at beginning to maintain parent->child order)
		hierarchyPath = append([]*queue_info.QueueInfo{currentQueue}, hierarchyPath...)
		currentQueue = r.queues[currentQueue.ParentQueue]
	}

	return hierarchyPath
}

func (r *resolver) cachePreemptMinRuntime(queue *queue_info.QueueInfo, minRuntime metav1.Duration) {
	if r.preemptMinRuntimeCache == nil {
		return
	}
	r.preemptMinRuntimeCache[queue.UID] = minRuntime
}

func (r *resolver) cacheReclaimMinRuntime(preemptorQueue *queue_info.QueueInfo, preempteeQueue *queue_info.QueueInfo, minRuntime metav1.Duration) {
	if r.reclaimMinRuntimeCache == nil {
		return
	}

	if r.reclaimMinRuntimeCache[preemptorQueue.UID] == nil {
		r.reclaimMinRuntimeCache[preemptorQueue.UID] = make(map[common_info.QueueID]metav1.Duration)
	}
	r.reclaimMinRuntimeCache[preemptorQueue.UID][preempteeQueue.UID] = minRuntime
}
