// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_order

import (
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	rs "github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

const (
	lQueuePrioritized   = -1
	rQueuePrioritized   = 1
	equalPrioritization = 0
)

func GetQueueOrderResult(
	lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes,
	lJobInfo, rJobInfo *podgroup_info.PodGroupInfo,
	lVictims, rVictims []*podgroup_info.PodGroupInfo,
	taskOrderFn common_info.LessFn, totalResources rs.ResourceQuantities,
	minNodeGPUMemory int64,
) int {
	var result int
	// queues that are currently utilize more than their fair share will be ordered after queues that are under utilize their fair share (based on api.LessFn)
	result = prioritizeUnderUtilized(lQueue, rQueue)
	if result != equalPrioritization {
		return result
	}

	// queues that are currently utilizing more than their deserved quota will be ordered after queues that are under
	// their deserved quota, to match GuaranteeDeservedQuotaStrategy (based on api.LessFn)
	result = prioritizeUnderQuotaWithJob(lQueue, rQueue, lJobInfo, rJobInfo, taskOrderFn, minNodeGPUMemory)
	if result != equalPrioritization {
		return result
	}

	// queues with lower priority will be located further in the priority queue
	result = prioritizePrioritized(lQueue, rQueue)
	if result != equalPrioritization {
		return result
	}

	// penalize queues that use resources while having no allocatable share
	result = penalizeZeroShareWithJob(lQueue, rQueue, lJobInfo, rJobInfo, taskOrderFn, minNodeGPUMemory)
	if result != equalPrioritization {
		return result
	}

	// queues with higher dominant resource share will be located further in the priority queue
	result = prioritizeSmallerResourceShare(lQueue, rQueue, lJobInfo, rJobInfo, lVictims, rVictims, taskOrderFn, totalResources, minNodeGPUMemory)
	if result != equalPrioritization {
		return result
	}

	result = prioritizeSmallerResourceShareWithoutTask(lQueue, rQueue, totalResources)
	if result != equalPrioritization {
		return result
	}

	// queues with larger allocatable share will be located further in the priority queue
	// prioritizing the "weaker" queues
	result = prioritizeBasedOnAllocatableShare(lQueue, rQueue)
	if result != equalPrioritization {
		return result
	}

	// Last resort, give Priority to the queue who was created first as a tie-breaker
	return prioritizeBasedOnCreationTime(lQueue, rQueue)
}

func prioritizePrioritized(lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes) int {
	if lQueue.Priority > rQueue.Priority {
		return lQueuePrioritized
	}

	if lQueue.Priority < rQueue.Priority {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeUnderUtilized(lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes) int {
	lOverUtilized := lQueue.GetFairShare().Less(lQueue.GetAllocatedShare())
	rOverUtilized := rQueue.GetFairShare().Less(rQueue.GetAllocatedShare())

	if !lOverUtilized && rOverUtilized {
		return lQueuePrioritized
	}

	if lOverUtilized && !rOverUtilized {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeUnderQuotaWithJob(lQueue, rQueue *rs.QueueAttributes,
	lJobInfo, rJobInfo *podgroup_info.PodGroupInfo,
	taskOrderFn common_info.LessFn, minNodeGPUMemory int64) int {

	lAllocatedWithJob := lQueue.GetAllocatedShare()
	lJobRequirements := utils.QuantifyResource(
		podgroup_info.GetTasksToAllocateInitResource(lJobInfo, taskOrderFn, false, minNodeGPUMemory))
	lAllocatedWithJob.Add(lJobRequirements)

	rAllocatedWithJob := rQueue.GetAllocatedShare()
	rJobRequirements := utils.QuantifyResource(
		podgroup_info.GetTasksToAllocateInitResource(rJobInfo, taskOrderFn, false, minNodeGPUMemory))
	rAllocatedWithJob.Add(rJobRequirements)

	lStarved := lAllocatedWithJob.LessEqual(lQueue.GetDeservedShare())
	rStarved := rAllocatedWithJob.LessEqual(rQueue.GetDeservedShare())

	if lStarved && !rStarved {
		return lQueuePrioritized
	}

	if rStarved && !lStarved {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func penalizeZeroShareWithJob(
	lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes,
	lJobInfo, rJobInfo *podgroup_info.PodGroupInfo,
	taskOrderFn common_info.LessFn, minNodeGPUMemory int64,
) int {
	lAllocatedWithJob := lQueue.GetAllocatedShare()
	lJobRequirements := utils.QuantifyResource(
		podgroup_info.GetTasksToAllocateInitResource(lJobInfo, taskOrderFn, false, minNodeGPUMemory))
	lAllocatedWithJob.Add(lJobRequirements)

	rAllocatedWithJob := rQueue.GetAllocatedShare()
	rJobRequirements := utils.QuantifyResource(
		podgroup_info.GetTasksToAllocateInitResource(rJobInfo, taskOrderFn, false, minNodeGPUMemory))
	rAllocatedWithJob.Add(rJobRequirements)

	lQueueViolation := false
	for _, resource := range rs.AllResources {
		allocatableShare := lQueue.GetAllocatableShare()[resource]
		if allocatableShare != 0 {
			continue
		}

		allocatedShare := lAllocatedWithJob[resource]
		if allocatedShare > 0 {
			lQueueViolation = true
		}
	}

	rQueueViolation := false
	for _, resource := range rs.AllResources {
		allocatableShare := rQueue.GetAllocatableShare()[resource]
		if allocatableShare != 0 {
			continue
		}

		allocatedShare := rAllocatedWithJob[resource]
		if allocatedShare > 0 {
			rQueueViolation = true
		}
	}

	if lQueueViolation && !rQueueViolation {
		return rQueuePrioritized
	}

	if !lQueueViolation && rQueueViolation {
		return lQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeSmallerResourceShare(
	lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes,
	lJobInfo, rJobInfo *podgroup_info.PodGroupInfo,
	lVictims, rVictims []*podgroup_info.PodGroupInfo,
	taskOrderFn common_info.LessFn, totalResources rs.ResourceQuantities,
	minNodeGPUMemory int64,
) int {
	lShare := calculateDominantResourceShareWithJob(lQueue, lJobInfo, lVictims, taskOrderFn, totalResources, minNodeGPUMemory)
	rShare := calculateDominantResourceShareWithJob(rQueue, rJobInfo, rVictims, taskOrderFn, totalResources, minNodeGPUMemory)

	if lShare < rShare {
		return lQueuePrioritized
	}

	if lShare > rShare {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeSmallerResourceShareWithoutTask(
	lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes,
	totalResources rs.ResourceQuantities,
) int {
	lShare := lQueue.GetDominantResourceShare(totalResources)
	rShare := rQueue.GetDominantResourceShare(totalResources)

	if lShare < rShare {
		return lQueuePrioritized
	}

	if lShare > rShare {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeBasedOnAllocatableShare(lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes) int {
	lAllocatableShare := lQueue.GetAllocatableShare()
	rAllocatableShare := rQueue.GetAllocatableShare()
	if lAllocatableShare.LessInAtLeastOneResource(rAllocatableShare) && lAllocatableShare.LessEqual(rAllocatableShare) {
		return lQueuePrioritized
	}

	if rAllocatableShare.LessInAtLeastOneResource(lAllocatableShare) && rAllocatableShare.LessEqual(lAllocatableShare) {
		return rQueuePrioritized
	}

	return equalPrioritization
}

func prioritizeBasedOnCreationTime(lQueue *rs.QueueAttributes, rQueue *rs.QueueAttributes) int {
	if lQueue.CreationTimestamp.Before(&rQueue.CreationTimestamp) {
		return lQueuePrioritized
	}
	return rQueuePrioritized
}

func calculateDominantResourceShareWithJob(
	queueAttributes *rs.QueueAttributes, jobInfo *podgroup_info.PodGroupInfo,
	victims []*podgroup_info.PodGroupInfo, taskOrderFn common_info.LessFn, totalResources rs.ResourceQuantities,
	minNodeGPUMemory int64,
) float64 {
	allocatedShare := queueAttributes.GetAllocatedShare()

	initResQuantities := utils.QuantifyResource(
		podgroup_info.GetTasksToAllocateInitResource(jobInfo, taskOrderFn, false, minNodeGPUMemory))

	for _, resource := range rs.AllResources {
		resourceShare := queueAttributes.ResourceShare(resource)
		resourceShare.Allocated += initResQuantities[resource]
	}

	for _, victim := range victims {
		for _, resource := range rs.AllResources {
			resourceShare := queueAttributes.ResourceShare(resource)
			resourceShare.Allocated -= utils.QuantifyResource(victim.Allocated)[resource]
		}
	}

	share := queueAttributes.GetDominantResourceShare(totalResources)

	for _, resource := range rs.AllResources {
		resourceShare := queueAttributes.ResourceShare(resource)
		resourceShare.Allocated = allocatedShare[resource]
	}

	return share
}
