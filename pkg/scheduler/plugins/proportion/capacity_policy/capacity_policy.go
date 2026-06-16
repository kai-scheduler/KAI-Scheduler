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
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

type capacityCheckFn func(requestedShare rs.ResourceQuantities, job *podgroup_info.PodGroupInfo) *api.SchedulableResult

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

	// Semi-preemptible jobs only count core pod resources against non-preemptible quota.
	nonPreemptibleShare := requestedShareQuantities
	if job.IsSemiPreemptibleJob() {
		nonPreemptibleShare = cp.getCoreRequiredQuota(tasksToAllocate, job)
	}
	return cp.resultsWithNonPreemptibleOverQuota(nonPreemptibleShare, job)
}

func (cp *CapacityPolicy) IsNonPreemptibleJobOverQuota(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {

	requestedShareQuantities := getRequiredQuota(tasksToAllocate, cp.maxNodeGPUMemoryMiB)

	// Semi-preemptible jobs only count core pod resources against non-preemptible quota.
	if job.IsSemiPreemptibleJob() {
		requestedShareQuantities = cp.getCoreRequiredQuota(tasksToAllocate, job)
	}
	checkFns := []capacityCheckFn{cp.resultsWithNonPreemptibleOverQuota}
	return cp.isJobOverCapacity(requestedShareQuantities, job, checkFns)
}

func (cp *CapacityPolicy) IsTaskAllocationOnNodeOverCapacity(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
	node *node_info.NodeInfo) *api.SchedulableResult {
	requiredInitQuota := node.GetRequiredInitQuota(task)
	requestedShare := rs.NewResourceQuantities(
		requiredInitQuota[resource_info.CPUIndex],
		requiredInitQuota[resource_info.MemoryIndex],
		requiredInitQuota[resource_info.GPUIndex])

	// Elastic tasks of a semi-preemptible job are not subject to non-preemptible quota.
	if job.IsSemiPreemptibleJob() && !isTaskCoreForSemiPreemptibleJob(task, job) {
		return cp.resultsOverLimit(requestedShare, job)
	}
	checkFns := []capacityCheckFn{cp.resultsOverLimit, cp.resultsWithNonPreemptibleOverQuota}
	return cp.isJobOverCapacity(requestedShare, job, checkFns)
}

func (cp *CapacityPolicy) isJobOverCapacity(requestedShare rs.ResourceQuantities, job *podgroup_info.PodGroupInfo,
	checkFns []capacityCheckFn) *api.SchedulableResult {
	for _, checkFn := range checkFns {
		result := checkFn(requestedShare, job)
		if !result.IsSchedulable {
			log.InfraLogger.V(5).Infof("Job: <%v/%v> is over capacity. Reason: %v", job.Namespace, job.Name, result.Message)
			return result
		}
	}

	return Schedulable()
}

// getCoreRequiredQuota computes the non-preemptible resource quota for a semi-preemptible job,
// counting only core tasks (up to minMember per PodSet, accounting for already-allocated tasks).
func (cp *CapacityPolicy) getCoreRequiredQuota(tasksToAllocate []*pod_info.PodInfo, job *podgroup_info.PodGroupInfo) rs.ResourceQuantities {
	return getRequiredQuota(filterCoreTasksToAllocate(tasksToAllocate, job), cp.minNodeGPUMemory)
}

// filterCoreTasksToAllocate returns the subset of tasksToAllocate that are "core" (non-preemptible)
// for a semi-preemptible job. A task is core if its PodSet has fewer than minMember allocated tasks.
func filterCoreTasksToAllocate(tasksToAllocate []*pod_info.PodInfo, job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	tasksByPodSet := map[string][]*pod_info.PodInfo{}
	for _, task := range tasksToAllocate {
		name := task.SubGroupName
		if name == "" {
			name = podgroup_info.DefaultSubGroup
		}
		tasksByPodSet[name] = append(tasksByPodSet[name], task)
	}

	var coreTasks []*pod_info.PodInfo
	for podSetName, tasks := range tasksByPodSet {
		podSet, found := job.PodSets[podSetName]
		if !found {
			coreTasks = append(coreTasks, tasks...)
			continue
		}
		coreCount := int(podSet.GetMinAvailable()) - podSet.GetNumActiveAllocatedTasks()
		if coreCount > len(tasks) {
			coreCount = len(tasks)
		}
		if coreCount > 0 {
			coreTasks = append(coreTasks, tasks[:coreCount]...)
		}
	}
	return coreTasks
}

// isTaskCoreForSemiPreemptibleJob returns true if the task is a core (non-preemptible) task
// in its PodSet, i.e. the PodSet has fewer allocated tasks than minMember.
func isTaskCoreForSemiPreemptibleJob(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) bool {
	podSetName := task.SubGroupName
	if podSetName == "" {
		podSetName = podgroup_info.DefaultSubGroup
	}
	podSet, found := job.PodSets[podSetName]
	if !found {
		return true
	}
	return podSet.GetNumActiveAllocatedTasks() < int(podSet.GetMinAvailable())
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
