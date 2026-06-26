// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package strategies

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

type MaintainFairShareStrategy struct{}
type GuaranteeDeservedQuotaStrategy struct{}

var strategies = []ReclaimStrategy{&MaintainFairShareStrategy{}, &GuaranteeDeservedQuotaStrategy{}}

func FitsReclaimStrategy(
	reclaimerResources resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities,
) bool {
	for _, strategy := range strategies {
		if strategy.Reclaimable(
			reclaimerResources, vectorMap, reclaimerQueue, reclaimeeQueue,
			reclaimeeRemainingShare,
		) {
			return true
		}
	}
	return false
}

type ReclaimStrategy interface {
	Reclaimable(
		reclaimerResources resource_info.ResourceVector, vectorMap *resource_info.ResourceVectorMap,
		reclaimerQueue *rs.QueueAttributes,
		reclaimeeQueue *rs.QueueAttributes, reclaimeeRemainingShare rs.ResourceQuantities,
	) bool
}

func (mfss *MaintainFairShareStrategy) Reclaimable(
	_ resource_info.ResourceVector,
	_ *resource_info.ResourceVectorMap,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities) bool {
	// This strategy allows to reclaim if reclaimee is currently over allowed fair share

	log.InfraLogger.V(6).Infof("Checking if reclaim is possible for reclaimer <%s> and reclaimee <%s> in order "+
		"to maintain fair share. Reclaimee requested: <%s>, deserved: <%s>, fairShare: <%s>, "+
		"reclaimeeRemainingShare: <%s>",
		reclaimerQueue.Name, reclaimeeQueue.Name, reclaimeeQueue.GetRequestableShare(), reclaimeeQueue.GetDeservedShare(),
		reclaimeeQueue.GetFairShare(), reclaimeeRemainingShare)

	return FitsMaintainFairShare(reclaimeeQueue, reclaimeeRemainingShare)
}

func (gdqs *GuaranteeDeservedQuotaStrategy) Reclaimable(
	reclaimerResources resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities) bool {
	// This strategy allows to reclaim if reclaimer is under deserved quota ("starved") and reclaimer is above quota

	log.InfraLogger.V(6).Infof("Checking if reclaim is possible for reclaimer <%s> and reclaimee <%s> in order to "+
		"Guarantee deserved quota. "+
		"Reclaimee requested: <%s>, deserved: <%s>, fairShare: <%s>, reclaimeeRemainingShare: <%s> "+
		"Reclaimer requested: <%s>, deserved: <%s>, fairShare: <%s>",
		reclaimerQueue.Name, reclaimeeQueue.Name, reclaimeeQueue.GetRequestableShare(), reclaimeeQueue.GetDeservedShare(),
		reclaimeeQueue.GetFairShare(), reclaimeeRemainingShare, reclaimerQueue.GetRequestableShare(),
		reclaimerQueue.GetDeservedShare(), reclaimerQueue.GetFairShare())

	// reclaimer has to be under (or equal) deserved quota in all resources (cpu, mem, gpu)
	if !ReclaimerFitsDeservedQuota(reclaimerResources, vectorMap, reclaimerQueue) {
		return false
	}

	// reclaimee should be over deserved quota (at least in one of the resources)
	if !ReclaimeeExceedsDeservedQuota(reclaimeeQueue, reclaimeeRemainingShare) {
		return false
	}

	return true
}

// FitsMaintainFairShare returns true when the reclaimee remains over its allocatable share.
func FitsMaintainFairShare(reclaimeeQueue *rs.QueueAttributes, reclaimeeRemainingShare rs.ResourceQuantities) bool {
	return !reclaimeeRemainingShare.LessEqual(reclaimeeQueue.GetAllocatableShare())
}

// ReclaimerFitsDeservedQuota returns true when adding the reclaimer keeps its queue within deserved quota.
func ReclaimerFitsDeservedQuota(
	reclaimerResources resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	reclaimerQueue *rs.QueueAttributes,
) bool {
	reclaimerRequestedQuota := reclaimerQueue.GetAllocatedShare()
	reclaimerRequestedQuota.Add(utils.QuantifyVector(reclaimerResources, vectorMap))

	return reclaimerRequestedQuota.LessEqual(reclaimerQueue.GetDeservedShare())
}

// ReclaimeeExceedsDeservedQuota returns true when the reclaimee remains over deserved quota.
func ReclaimeeExceedsDeservedQuota(
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities,
) bool {
	return !reclaimeeRemainingShare.LessEqual(reclaimeeQueue.GetDeservedShare())
}
