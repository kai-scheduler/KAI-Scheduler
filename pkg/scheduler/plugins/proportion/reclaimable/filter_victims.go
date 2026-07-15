// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/reclaimable/strategies"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
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

	return canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
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
