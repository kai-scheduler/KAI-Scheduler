// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

type CapacityPolicy struct {
	queues              map[common_info.QueueID]*rs.QueueAttributes
	maxNodeGPUMemoryMiB *int64
}

func New(queues map[common_info.QueueID]*rs.QueueAttributes, maxNodeGPUMemoryMiB *int64) *CapacityPolicy {
	return &CapacityPolicy{queues, maxNodeGPUMemoryMiB}
}

func (cp *CapacityPolicy) IsJobOverQueueCapacity(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {
	requestedShareQuantities := getRequiredQuota(tasksToAllocate, cp.maxNodeGPUMemoryMiB)

	if result := cp.resultsOverLimit(requestedShareQuantities, job); !result.IsSchedulable {
		return result
	}
	// Semi-preemptible: only the core portion of the batch counts against the non-preemptible quota.
	nonPreemptibleShare := cp.getCoreRequiredQuota(requestedShareQuantities, tasksToAllocate, job)
	return cp.resultsWithNonPreemptibleOverQuota(nonPreemptibleShare, job)
}

func (cp *CapacityPolicy) IsNonPreemptibleJobOverQuota(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {

	requestedShareQuantities := getRequiredQuota(tasksToAllocate, cp.maxNodeGPUMemoryMiB)
	nonPreemptibleShare := cp.getCoreRequiredQuota(requestedShareQuantities, tasksToAllocate, job)
	return cp.resultsWithNonPreemptibleOverQuota(nonPreemptibleShare, job)
}

func (cp *CapacityPolicy) IsTaskAllocationOnNodeOverCapacity(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
	node *node_info.NodeInfo) *api.SchedulableResult {
	requiredInitQuota := node.GetRequiredInitQuota(task)
	requestedShare := rs.NewResourceQuantities(
		requiredInitQuota[resource_info.CPUIndex],
		requiredInitQuota[resource_info.MemoryIndex],
		requiredInitQuota[resource_info.GPUIndex])

	if result := cp.resultsOverLimit(requestedShare, job); !result.IsSchedulable {
		return result
	}
	// For a semi-preemptible job already at its minimum, the incoming task is elastic and does not
	// count against the non-preemptible quota; only the over-limit check applies.
	nonPreemptibleShare := requestedShare
	if job.IsSemiPreemptibleJob() && podgroup_info.IsMinRequirementSatisfied(job) {
		nonPreemptibleShare = rs.EmptyResourceQuantities()
	}
	return cp.resultsWithNonPreemptibleOverQuota(nonPreemptibleShare, job)
}

// getCoreRequiredQuota returns the not-preemptible quota to charge for a batch of tasks. For non
// semi-preemptible jobs it is the full requested share. For a semi-preemptible job it is phase-based:
// once the job's minimum is satisfied, the incoming batch is elastic burst and charges nothing;
// otherwise (gang phase) the whole batch is core and charges the full share.
func (cp *CapacityPolicy) getCoreRequiredQuota(
	requestedShare rs.ResourceQuantities, tasksToAllocate []*pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
) rs.ResourceQuantities {
	if !job.IsSemiPreemptibleJob() {
		return requestedShare
	}
	if podgroup_info.IsMinRequirementSatisfied(job) {
		return rs.EmptyResourceQuantities()
	}
	return getRequiredQuota(tasksToAllocate, cp.maxNodeGPUMemoryMiB)
}

// getRequiredQuota calculates the required quota for a job based on the tasks to allocate and the max node GPU memory.
// The function uses max gpu memory seen in the cluster to calculate the most conservative option for a quota of a work with gpu memory request.
// max divisor → smallest fraction. If even the smallest fraction is passed the limit, we can say that the pod is over the limit right now, without simulations.
func getRequiredQuota(tasksToAllocate []*pod_info.PodInfo, maxNodeGPUMemory *int64) rs.ResourceQuantities {
	quota := rs.EmptyResourceQuantities()
	for _, pod := range tasksToAllocate {
		quantities := utils.QuantifyVector(pod.ResReqVector, pod.VectorMap)
		quota[rs.CpuResource] += quantities[rs.CpuResource]
		quota[rs.MemoryResource] += quantities[rs.MemoryResource]
		if pod.IsGpuMemoryRequest() {
			if maxNodeGPUMemory != nil {
				quota[rs.GpuResource] += pod.GpuRequirement.GpuMemoryAsGpuFraction(*maxNodeGPUMemory)
			}
		} else {
			quota[rs.GpuResource] += quantities[rs.GpuResource]
		}
	}
	return quota
}
