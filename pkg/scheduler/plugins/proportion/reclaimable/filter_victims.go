// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/reclaimable/strategies"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

// FilterVictim removes victims that cannot be reclaimed by any proportion reclaim strategy.
func (r *Reclaimable) FilterVictim(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
	reclaimeeQueueID common_info.QueueID,
) bool {
	if reclaimer == nil {
		return true
	}

	reclaimerQueue, reclaimeeQueue := r.getLeveledQueues(queues, reclaimer.Queue, reclaimeeQueueID)
	if reclaimerQueue == nil || reclaimeeQueue == nil {
		return true
	}

	if !strategies.ReclaimerFitsDeservedQuota(reclaimer.RequiredResources, reclaimer.VectorMap, reclaimerQueue) {
		return strategies.FitsMaintainFairShare(reclaimeeQueue, reclaimeeQueue.GetAllocatedShare())
	}

	if strategies.ReclaimerFitsReclaimByQueuePriority(r.queuePriorityInQuotaReclaim, reclaimerQueue, reclaimeeQueue) {
		return true
	}

	return canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
}

// FilterVictimWithSteadyFairShare is FilterVictim with steady fair-share reclaim enabled. A reclaimer the
// existing strategies apply to keeps their victims; a reclaimer below its steady fair share may also take
// victims from queues above theirs. victimBatch returns what reclaim would evict from the victim first;
// a victim whose first batch alone would take its queue below its steady fair share is left out, so the
// search moves on to the queue's smaller jobs.
func (r *Reclaimable) FilterVictimWithSteadyFairShare(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
	reclaimeeQueueID common_info.QueueID,
	victimBatch func() rs.ResourceQuantities,
) bool {
	if reclaimer == nil {
		return true
	}
	leveledReclaimerQueue, reclaimeeQueue := r.getLeveledQueues(queues, reclaimer.Queue, reclaimeeQueueID)
	if leveledReclaimerQueue == nil || reclaimeeQueue == nil {
		return true
	}

	reclaimerQueue := queues[reclaimer.Queue]
	if reclaimerFitsFairShare(reclaimerQueue, utils.QuantifyVector(reclaimer.RequiredResources, reclaimer.VectorMap)) &&
		r.FilterVictim(queues, reclaimer, reclaimeeQueueID) {
		return true
	}
	if !r.reclaimerBelowSteadyFairShare(reclaimerQueue, reclaimer) ||
		!strategies.ReclaimerBelowSteadyFairShare(reclaimer.RequiredResources, reclaimer.VectorMap, leveledReclaimerQueue) {
		return false
	}
	remaining := reclaimeeQueue.GetAllocatedShare()
	remaining.Sub(victimBatch())
	return strategies.ReclaimeeKeepsSteadyFairShare(reclaimeeQueue, remaining)
}

func canBeDeservedQuotaReclaimCandidate(
	reclaimer *ReclaimerInfo, reclaimeeQueue *rs.QueueAttributes,
) bool {
	hasUnderDeservedResource := false
	for _, resource := range rs.AllResources {
		if rs.ResourceQuantityFromVector(resource, reclaimer.RequiredResources, reclaimer.VectorMap) <= 0 {
			continue
		}

		resourceShare := reclaimeeQueue.ResourceShare(resource)
		if resourceShare.Deserved == commonconstants.UnlimitedResourceQuantity {
			continue
		}
		if resourceShare.Allocated > resourceShare.Deserved {
			return true
		}
		if resourceShare.Allocated < resourceShare.Deserved {
			hasUnderDeservedResource = true
		}
	}

	return !hasUnderDeservedResource
}
