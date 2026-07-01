// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
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
	allocated := reclaimeeQueue.GetAllocatedShare()
	deserved := reclaimeeQueue.GetDeservedShare()
	involvedResources := getInvolvedResourcesNames([]resource_info.ResourceVector{reclaimer.RequiredResources}, reclaimer.VectorMap)

	hasUnderDeservedResource := false
	for resource := range involvedResources {
		if deserved[resource] == commonconstants.UnlimitedResourceQuantity {
			continue
		}
		if allocated[resource] > deserved[resource] {
			return true
		}
		if allocated[resource] < deserved[resource] {
			hasUnderDeservedResource = true
		}
	}

	return !hasUnderDeservedResource
}
