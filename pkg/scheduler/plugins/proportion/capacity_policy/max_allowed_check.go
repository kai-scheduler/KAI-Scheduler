// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package capacity_policy

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	rs "github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/proportion/resource_share"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/proportion/utils"
)

func (cp *CapacityPolicy) resultsOverLimit(requestedShare rs.ResourceQuantities,
	job *podgroup_info.PodGroupInfo) *api.SchedulableResult {

	for queueAttributes, ok := cp.queues[job.Queue]; ok; queueAttributes, ok = cp.queues[queueAttributes.ParentQueue] {
		overLimit, exceedingResourceName := isOverLimit(queueAttributes, requestedShare)
		if overLimit {
			request := utils.ResourceRequirementsFromQuantities(requestedShare).ToResourceList()
			requestedNonPreemptible := resource_info.EmptyResourceRequirements().ToResourceList()
			if !job.IsPreemptibleJob() {
				requestedNonPreemptible = request.DeepCopy()
			}

			return &api.SchedulableResult{
				IsSchedulable: false,
				Reason:        v2alpha2.OverLimit,
				Message:       getQueueOverLimitError(exceedingResourceName, queueAttributes, requestedShare),
				Details: &v2alpha2.UnschedulableExplanationDetails{
					QueueDetails: &v2alpha2.QuotaDetails{
						Name:                                     string(queueAttributes.UID),
						QueueRequestedResources:                  utils.ResourceRequirementsFromQuantities(queueAttributes.GetRequestShare()).ToResourceList(),
						QueueDeservedResources:                   utils.ResourceRequirementsFromQuantities(queueAttributes.GetDeservedShare()).ToResourceList(),
						QueueAllocatedResources:                  utils.ResourceRequirementsFromQuantities(queueAttributes.GetAllocatedShare()).ToResourceList(),
						QueueAllocatedNonPreemptibleResources:    utils.ResourceRequirementsFromQuantities(queueAttributes.GetAllocatedNonPreemptible()).ToResourceList(),
						QueueResourceLimits:                      utils.ResourceRequirementsFromQuantities(queueAttributes.GetMaxAllowedShare()).ToResourceList(),
						PodGroupRequestedResources:               request,
						PodGroupRequestedNonPreemptibleResources: requestedNonPreemptible,
					},
				},
			}
		}
	}

	return Schedulable()
}

func isOverLimit(queueAttributes *rs.QueueAttributes, requested rs.ResourceQuantities) (bool, rs.ResourceName) {
	for _, resource := range rs.AllResources {
		resourceShare := queueAttributes.ResourceShare(resource)
		if resourceShare.MaxAllowed == constants.UnlimitedResourceQuantity {
			continue
		}
		requestedQty, found := requested[resource]
		if !found || requestedQty == 0 {
			continue
		}
		if resourceShare.MaxAllowed < resourceShare.Allocated+requestedQty {
			return true, resource
		}
	}
	return false, ""
}

func getQueueOverLimitError(resourceName rs.ResourceName, queueAttributes *rs.QueueAttributes,
	requestedShare rs.ResourceQuantities) string {
	maxAllowed := queueAttributes.GetMaxAllowedShare()[resourceName]
	allocated := queueAttributes.GetAllocatedShare()[resourceName]
	requested := requestedShare[resourceName]

	return api.GetJobOverMaxAllowedMessageForQueue(queueAttributes.Name, string(resourceName), maxAllowed, allocated,
		requested)
}
