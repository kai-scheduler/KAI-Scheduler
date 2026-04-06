// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	"fmt"
	"maps"
	"math"
	"strings"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/reclaimable/strategies"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

type Reclaimable struct {
	saturationMultiplier float64
}

func New(multiplier float64) *Reclaimable {
	return &Reclaimable{
		saturationMultiplier: multiplier,
	}
}

func (r *Reclaimable) CanReclaimResources(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
) bool {
	reclaimerQueue := queues[reclaimer.Queue]
	requestedResources := utils.QuantifyVector(reclaimer.RequiredResources, reclaimer.VectorMap)

	allocatedResources := reclaimerQueue.GetAllocatedShare()
	allocatedResources.Add(requestedResources)
	if !allocatedResources.LessEqual(reclaimerQueue.GetFairShare()) {
		return false
	}

	if reclaimer.IsPreemptable {
		return true
	}

	allocatedNonPreemptible := reclaimerQueue.GetAllocatedNonPreemptible()
	allocatedNonPreemptible.Add(requestedResources)
	if !allocatedNonPreemptible.LessEqual(reclaimerQueue.GetDeservedShare()) {
		return false
	}

	return true
}

func (r *Reclaimable) Reclaimable(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
	reclaimeeResourcesByQueue map[common_info.QueueID][]resource_info.ResourceVector,
) bool {
	reclaimable, reclaimedQueuesRemainingResources, involvedResources :=
		r.reclaimResourcesFromReclaimees(queues, reclaimer, reclaimeeResourcesByQueue)
	if !reclaimable {
		return false
	}
	return r.reclaimingQueuesRemainWithinBoundaries(queues, reclaimer, reclaimedQueuesRemainingResources, involvedResources)
}

func (r *Reclaimable) reclaimResourcesFromReclaimees(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
	reclaimeesResourcesByQueue map[common_info.QueueID][]resource_info.ResourceVector,
) (
	bool, map[common_info.QueueID]rs.ResourceQuantities, map[common_info.QueueID]map[rs.ResourceName]any,
) {
	involvedResourcesByQueue := map[common_info.QueueID]map[rs.ResourceName]any{}
	remainingResourcesMap := map[common_info.QueueID]rs.ResourceQuantities{}
	for reclaimeeQueueID, reclaimeeQueueReclaimedResources := range reclaimeesResourcesByQueue {
		reclaimerQueue, reclaimeeQueue := r.getLeveledQueues(queues, reclaimer.Queue, reclaimeeQueueID)

		involvedResourcesByQueue[reclaimeeQueueID] = getInvolvedResourcesNames(reclaimeeQueueReclaimedResources, reclaimer.VectorMap)

		if _, found := remainingResourcesMap[reclaimeeQueue.UID]; !found {
			remainingResourcesMap[reclaimeeQueue.UID] = queues[reclaimeeQueue.UID].GetAllocatedShare()
		}
		remainingResources := remainingResourcesMap[reclaimeeQueue.UID]

		for _, reclaimeeResources := range reclaimeeQueueReclaimedResources {
			if !strategies.FitsReclaimStrategy(reclaimer.RequiredResources, reclaimer.VectorMap, reclaimerQueue, reclaimeeQueue,
				remainingResources) {
				log.InfraLogger.V(7).Infof("queue <%s>，shouldn't be reclaimed, for %s resources"+
					" remaining reosurces: <%s>, deserved: <%s>, fairShare: <%s>",
					reclaimeeQueue.Name, stringVectorArray(reclaimeeQueueReclaimedResources, reclaimer.VectorMap),
					remainingResources, reclaimeeQueue.GetDeservedShare(),
					reclaimeeQueue.GetFairShare())
				return false, nil, nil
			}

			r.subtractReclaimedResources(queues, remainingResourcesMap, reclaimeeQueueID, reclaimeeResources, reclaimer.VectorMap, involvedResourcesByQueue)
		}
	}

	return true, remainingResourcesMap, involvedResourcesByQueue
}

func (r *Reclaimable) subtractReclaimedResources(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	remainingResourcesMap map[common_info.QueueID]rs.ResourceQuantities,
	reclaimeeQueueID common_info.QueueID,
	reclaimedResources resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	involvedResourcesByQueue map[common_info.QueueID]map[rs.ResourceName]any,
) {
	for queue, ok := queues[reclaimeeQueueID]; ok; queue, ok = queues[queue.ParentQueue] {
		if _, found := remainingResourcesMap[queue.UID]; !found {
			remainingResourcesMap[queue.UID] = queues[queue.UID].GetAllocatedShare()
		}

		remainingResources := remainingResourcesMap[queue.UID]
		activeAllocatedQuota := utils.QuantifyVector(reclaimedResources, vectorMap)
		remainingResources.Sub(activeAllocatedQuota)

		_, found := involvedResourcesByQueue[queue.UID]
		if found {
			maps.Copy(involvedResourcesByQueue[queue.UID], involvedResourcesByQueue[reclaimeeQueueID])
		} else {
			involvedResourcesByQueue[queue.UID] = maps.Clone(involvedResourcesByQueue[reclaimeeQueueID])
		}
	}
}

func (r *Reclaimable) reclaimingQueuesRemainWithinBoundaries(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimer *ReclaimerInfo,
	remainingResourcesMap map[common_info.QueueID]rs.ResourceQuantities,
	involvedResourcesByQueue map[common_info.QueueID]map[rs.ResourceName]any,
) bool {

	requestedQuota := utils.QuantifyVector(reclaimer.RequiredResources, reclaimer.VectorMap)
	reclaimerInvolvedResources := getInvolvedResourcesNames([]resource_info.ResourceVector{reclaimer.RequiredResources}, reclaimer.VectorMap)

	for reclaimingQueue, found := queues[reclaimer.Queue]; found; reclaimingQueue, found = queues[reclaimingQueue.ParentQueue] {
		remainingResources, foundRemaining := remainingResourcesMap[reclaimingQueue.UID]
		if !foundRemaining {
			remainingResources = reclaimingQueue.GetAllocatedShare()
		}
		remainingResources.Add(requestedQuota)

		for siblingID := range remainingResourcesMap {
			sibling := queues[siblingID]
			if sibling.ParentQueue != reclaimingQueue.ParentQueue || sibling.UID == reclaimingQueue.UID {
				continue
			}

			siblingQueueRemainingResources, foundSib := remainingResourcesMap[sibling.UID]
			if !foundSib {
				siblingQueueRemainingResources = sibling.GetAllocatedShare()
			}

			involvedResources := maps.Clone(involvedResourcesByQueue[siblingID])
			maps.Copy(involvedResources, reclaimerInvolvedResources)
			if !r.isFairShareSaturationLowerPerResource(
				involvedResources,
				remainingResources, reclaimingQueue.GetFairShare(),
				siblingQueueRemainingResources, sibling.GetFairShare(),
			) {
				log.InfraLogger.V(5).Infof("Failed to reclaim resources for job: <%s/%s>. "+
					"Saturation ratios would not stay lower than sibling queue <%s>",
					reclaimer.Namespace, reclaimer.Name, sibling.Name)
				return false
			}
		}

		if reclaimer.IsPreemptable {
			continue
		}

		allocatedNonPreemptible := reclaimingQueue.GetAllocatedNonPreemptible()
		allocatedNonPreemptible.Add(requestedQuota)
		if !allocatedNonPreemptible.LessEqual(reclaimingQueue.GetDeservedShare()) {
			log.InfraLogger.V(5).Infof("Failed to reclaim resources for: <%s/%s> in queue <%s>. "+
				"Queue will have nonpreemtible jobs over quota and reclaimer job is an interactive job. "+
				"Queue quota: %s, queue allocated nonpreemtible resources with task: %s",
				reclaimer.Namespace, reclaimer.Name, reclaimingQueue.Name, reclaimingQueue.GetDeservedShare(),
				allocatedNonPreemptible)
			return false
		}
	}

	return true
}

// isFairShareSaturationLowerPerResource returns true if for every resource the
// saturation ratio (allocated / fairShare) of the reclaiming queue is strictly
// lower than the utilisation ratio of the sibling queue after being multiplied by the saturationMultiplier.
// A comparison for a given resource is skipped when both queues have unlimited
// fair share configured for that resource.
func (r *Reclaimable) isFairShareSaturationLowerPerResource(
	involvedResources map[rs.ResourceName]any,
	reclaimerAllocated rs.ResourceQuantities, reclaimerFair rs.ResourceQuantities,
	siblingAlloc rs.ResourceQuantities, siblingFair rs.ResourceQuantities,
) bool {
	for resource := range involvedResources {
		reclaimerFairShare := reclaimerFair[resource]
		siblingFairShare := siblingFair[resource]

		if reclaimerFairShare == commonconstants.UnlimitedResourceQuantity && siblingFairShare == commonconstants.UnlimitedResourceQuantity {
			continue
		}

		ratioReclaimer := fairShareSaturationRatio(reclaimerAllocated[resource], reclaimerFairShare)
		ratioSibling := fairShareSaturationRatio(siblingAlloc[resource], siblingFairShare)

		if (ratioReclaimer > 1) && (siblingFairShare > 0) && ((ratioReclaimer * r.saturationMultiplier) >= ratioSibling) {
			return false
		}
	}
	return true
}

// fairShareSaturationRatio computes allocated/fairShare ratio while handling
// edge cases.
func fairShareSaturationRatio(allocated float64, fairShare float64) float64 {
	if fairShare == 0 {
		if allocated > 0 {
			return math.Inf(1)
		}
		return 0
	}
	if fairShare == commonconstants.UnlimitedResourceQuantity {
		return 0
	}
	return allocated / fairShare
}

func (r *Reclaimable) getLeveledQueues(
	queues map[common_info.QueueID]*rs.QueueAttributes,
	reclaimerQueueID common_info.QueueID,
	reclaimeeQueueID common_info.QueueID,
) (*rs.QueueAttributes, *rs.QueueAttributes) {
	reclaimers := r.getHierarchyPath(queues, reclaimerQueueID)
	reclaimees := r.getHierarchyPath(queues, reclaimeeQueueID)

	minLength := int(math.Min(float64(len(reclaimers)), float64(len(reclaimees))))
	var reclaimerQueue, reclaimeeQueue *rs.QueueAttributes
	for i := 0; i < minLength; i++ {
		reclaimerQueue = reclaimers[i]
		reclaimeeQueue = reclaimees[i]
		if reclaimerQueue.UID != reclaimeeQueue.UID {
			break
		}
	}
	return reclaimerQueue, reclaimeeQueue
}

func (r *Reclaimable) getHierarchyPath(
	queues map[common_info.QueueID]*rs.QueueAttributes, queueId common_info.QueueID) []*rs.QueueAttributes {
	var hierarchyPath []*rs.QueueAttributes
	queue, found := queues[queueId]
	for found {
		hierarchyPath = append([]*rs.QueueAttributes{queue}, hierarchyPath...)
		queue, found = queues[queue.ParentQueue]
	}
	return hierarchyPath
}

func getInvolvedResourcesNames(resources []resource_info.ResourceVector, vectorMap *resource_info.ResourceVectorMap) map[rs.ResourceName]any {
	involvedResources := map[rs.ResourceName]any{}
	for _, vec := range resources {
		if vec == nil {
			continue
		}

		if vec.Get(resource_info.CPUIndex) > 0 {
			involvedResources[rs.CpuResource] = struct{}{}
		}

		if vec.Get(resource_info.MemoryIndex) > 0 {
			involvedResources[rs.MemoryResource] = struct{}{}
		}

		if vec.Get(resource_info.GPUIndex) > 0 {
			involvedResources[rs.GpuResource] = struct{}{}
		}
	}

	return involvedResources
}

func stringVectorArray(vectors []resource_info.ResourceVector, vectorMap *resource_info.ResourceVectorMap) string {
	if len(vectors) == 0 {
		return ""
	}
	parts := make([]string, 0, len(vectors))
	for _, vec := range vectors {
		parts = append(parts, fmt.Sprintf("%v", utils.QuantifyVector(vec, vectorMap)))
	}
	return strings.Join(parts, ",")
}
