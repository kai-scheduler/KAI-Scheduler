// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/utils"
)

func Schedulable() *api.SchedulableResult {
	return &api.SchedulableResult{
		IsSchedulable: true,
		Reason:        "",
		Message:       "",
		Details:       nil,
	}
}

func (cp *CapacityPolicy) resultsWithNonPreemptibleOverQuota(requestedShare rs.ResourceQuantities,
	job *podgroup_info.PodGroupInfo) *api.SchedulableResult {

	if job.IsPreemptibleJob() {
		log.InfraLogger.V(7).Infof("resultsWithNonPreemptibleOverQuota. Job: <%v/%v> is preemptable",
			job.Namespace, job.Name)
		return Schedulable()
	}

	for queueAttributes, ok := cp.queues[job.Queue]; ok; queueAttributes, ok = cp.queues[queueAttributes.ParentQueue] {
		overCapacity, exceedingResourceName := isAllocatedNonPreemptibleOverQuota(queueAttributes, requestedShare)
		if overCapacity {
			return &api.SchedulableResult{
				IsSchedulable: false,
				Reason:        v2alpha2.NonPreemptibleOverQuota,
				Message:       getNonPreemptibleJobOverQuotaError(queueAttributes, requestedShare, exceedingResourceName),
				Details: &v2alpha2.UnschedulableExplanationDetails{
					QueueDetails: &v2alpha2.QuotaDetails{
						Name:                                     string(queueAttributes.UID),
						QueueRequestedResources:                  utils.ResourceRequirementsFromQuantities(queueAttributes.GetRequestShare()).ToResourceList(),
						QueueDeservedResources:                   utils.ResourceRequirementsFromQuantities(queueAttributes.GetDeservedShare()).ToResourceList(),
						QueueAllocatedResources:                  utils.ResourceRequirementsFromQuantities(queueAttributes.GetAllocatedShare()).ToResourceList(),
						QueueAllocatedNonPreemptibleResources:    utils.ResourceRequirementsFromQuantities(queueAttributes.GetAllocatedNonPreemptible()).ToResourceList(),
						QueueResourceLimits:                      utils.ResourceRequirementsFromQuantities(queueAttributes.GetMaxAllowedShare()).ToResourceList(),
						PodGroupRequestedResources:               utils.ResourceRequirementsFromQuantities(requestedShare).ToResourceList(),
						PodGroupRequestedNonPreemptibleResources: utils.ResourceRequirementsFromQuantities(requestedShare).ToResourceList(),
					},
				},
			}
		}
	}

	return Schedulable()
}

func isAllocatedNonPreemptibleOverQuota(
	queueAttributes *rs.QueueAttributes, requested rs.ResourceQuantities,
) (bool, rs.ResourceName) {
	for _, resource := range rs.AllResources {
		resourceShare := queueAttributes.ResourceShare(resource)
		if resourceShare.Deserved == commonconstants.UnlimitedResourceQuantity {
			continue
		}
		requestedQty, found := requested[resource]
		if !found || requestedQty == 0 {
			continue
		}
		if resourceShare.Deserved < resourceShare.AllocatedNotPreemptible+requestedQty {
			return true, resource
		}
	}
	return false, ""
}

func getNonPreemptibleJobOverQuotaError(queueAttributes *rs.QueueAttributes, requestedQuota rs.ResourceQuantities,
	exceedingResourceName rs.ResourceName) string {
	deserved := queueAttributes.GetDeservedShare()[exceedingResourceName]
	allocatedNonPreemptible := queueAttributes.GetAllocatedNonPreemptible()[exceedingResourceName]
	jobReq := podgroup_info.JobRequirement{
		GPU:      requestedQuota[rs.GpuResource],
		MilliCPU: requestedQuota[rs.CpuResource],
		Memory:   requestedQuota[rs.MemoryResource],
	}
	return api.GetBuildOverCapacityMessageForQueue(queueAttributes.Name, string(exceedingResourceName), deserved,
		allocatedNonPreemptible, &jobReq)
}
