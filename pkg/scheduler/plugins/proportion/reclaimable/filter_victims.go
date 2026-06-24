// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

// FilterVictim removes victims that cannot be reclaimed by the deserved-quota strategy.
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

	if !canUseGuaranteeDeservedQuotaStrategy(reclaimer, reclaimerQueue) {
		return true
	}

	return canBeDeservedQuotaReclaimCandidate(reclaimer, reclaimeeQueue)
}

func canUseGuaranteeDeservedQuotaStrategy(
	reclaimer *ReclaimerInfo, reclaimerQueue *rs.QueueAttributes,
) bool {
	allocatedWithReclaimer := reclaimerQueue.GetAllocatedShare()
	allocatedWithReclaimer.Add(utils.QuantifyVector(reclaimer.RequiredResources, reclaimer.VectorMap))
	return allocatedWithReclaimer.LessEqual(reclaimerQueue.GetDeservedShare())
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
