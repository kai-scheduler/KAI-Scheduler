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
	reclaimerResources *resource_info.Resource,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities,
) bool {
	for _, strategy := range strategies {
		if strategy.Reclaimable(
			reclaimerResources, reclaimerQueue, reclaimeeQueue,
			reclaimeeRemainingShare,
		) {
			return true
		}
	}
	return false
}

type ReclaimStrategy interface {
	Reclaimable(
		reclaimerResources *resource_info.Resource, reclaimerQueue *rs.QueueAttributes,
		reclaimeeQueue *rs.QueueAttributes, reclaimeeRemainingShare rs.ResourceQuantities,
	) bool
}

func (mfss *MaintainFairShareStrategy) Reclaimable(
	_ *resource_info.Resource,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities) bool {
	// This strategy allows to reclaim if reclaimee is currently over allowed fair share

	log.InfraLogger.V(6).Infof("Checking if reclaim is possible for reclaimer <%v> and reclaimee <%v> in order "+
		"to maintain fair share. Reclaimee requested: <%v>, deserved: <%v>, fairShare: <%v>, "+
		"reclaimeeRemainingShare: <%v>",
		reclaimerQueue.Name, reclaimeeQueue.Name, reclaimeeQueue.GetRequestableShare().String(), reclaimeeQueue.GetDeservedShare().String(),
		reclaimeeQueue.GetFairShare().String(), reclaimeeRemainingShare.String())

	return !reclaimeeRemainingShare.LessEqual(reclaimeeQueue.GetAllocatableShare())
}

func (gdqs *GuaranteeDeservedQuotaStrategy) Reclaimable(
	reclaimerResources *resource_info.Resource,
	reclaimerQueue *rs.QueueAttributes,
	reclaimeeQueue *rs.QueueAttributes,
	reclaimeeRemainingShare rs.ResourceQuantities) bool {
	// This strategy allows to reclaim if reclaimer is under deserved quota ("starved") and reclaimer is above quota

	log.InfraLogger.V(6).Infof("Checking if reclaim is possible for reclaimer <%v> and reclaimee <%v> in order to "+
		"Guarantee deserved quota. "+
		"Reclaimee requested: <%v>, deserved: <%v>, fairShare: <%v>, reclaimeeRemainingShare: <%v> "+
		"Reclaimer requested: <%v>, deserved: <%v>, fairShare: <%v>",
		reclaimerQueue.Name, reclaimeeQueue.Name, reclaimeeQueue.GetRequestableShare().String(), reclaimeeQueue.GetDeservedShare().String(),
		reclaimeeQueue.GetFairShare().String(), reclaimeeRemainingShare.String(), reclaimerQueue.GetRequestableShare().String(),
		reclaimerQueue.GetDeservedShare().String(), reclaimerQueue.GetFairShare().String())

	// reclaimer has to be under (or equal) deserved quota in all resources (cpu, mem, gpu)
	if reclaimerWillGoOverQuota(reclaimerResources, reclaimerQueue) {
		return false
	}

	// reclaimee should be over deserved quota (at least in one of the resources)
	if reclaimeeRemainingShare.LessEqual(reclaimeeQueue.GetDeservedShare()) {
		return false
	}

	return true
}

func reclaimerWillGoOverQuota(reclaimerResources *resource_info.Resource, reclaimerQueue *rs.QueueAttributes) bool {
	reclaimerRequestedQuota := reclaimerQueue.GetAllocatedShare()
	reclaimerRequestedQuota.Add(utils.QuantifyResource(reclaimerResources))

	return !reclaimerRequestedQuota.LessEqual(reclaimerQueue.GetDeservedShare())
}
