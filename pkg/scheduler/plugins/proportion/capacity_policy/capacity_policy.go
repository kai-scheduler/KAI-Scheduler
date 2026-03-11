// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

type capacityCheckFn func(requestedShare rs.ResourceQuantities, job *podgroup_info.PodGroupInfo) *api.SchedulableResult

type CapacityPolicy struct {
	queues map[common_info.QueueID]*rs.QueueAttributes
}

func New(queues map[common_info.QueueID]*rs.QueueAttributes) *CapacityPolicy {
	return &CapacityPolicy{queues}
}

func (cp *CapacityPolicy) IsJobOverQueueCapacity(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {
	requiredQuota := getRequiredQuota(tasksToAllocate)
	requestedShareQuantities := rs.NewResourceQuantities(
		requiredQuota.MilliCPU,
		requiredQuota.Memory,
		requiredQuota.GPU)

	checkFns := []capacityCheckFn{cp.resultsOverLimit, cp.resultsWithNonPreemptibleOverQuota}
	return cp.isJobOverCapacity(requestedShareQuantities, job, checkFns)
}

func (cp *CapacityPolicy) IsNonPreemptibleJobOverQuota(job *podgroup_info.PodGroupInfo,
	tasksToAllocate []*pod_info.PodInfo) *api.SchedulableResult {

	requiredQuota := getRequiredQuota(tasksToAllocate)
	requestedShareQuantities := rs.NewResourceQuantities(
		requiredQuota.MilliCPU,
		requiredQuota.Memory,
		requiredQuota.GPU)

	checkFns := []capacityCheckFn{cp.resultsWithNonPreemptibleOverQuota}
	return cp.isJobOverCapacity(requestedShareQuantities, job, checkFns)
}

func (cp *CapacityPolicy) IsTaskAllocationOnNodeOverCapacity(task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo,
	node *node_info.NodeInfo) *api.SchedulableResult {
	requiredInitQuota := node.GetRequiredInitQuota(task)
	requestedShare := rs.NewResourceQuantities(
		requiredInitQuota.MilliCPU,
		requiredInitQuota.Memory,
		requiredInitQuota.GPU)

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

func getRequiredQuota(tasksToAllocate []*pod_info.PodInfo) *podgroup_info.JobRequirement {
	quota := podgroup_info.JobRequirement{}
	for _, pod := range tasksToAllocate {
		quota.GPU += pod.ResReq.GetGpusQuota()
		quota.MilliCPU += pod.ResReq.Cpu()
		quota.Memory += pod.ResReq.Memory()
	}
	return &quota
}
