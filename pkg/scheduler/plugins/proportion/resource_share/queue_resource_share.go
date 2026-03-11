// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"math"
	"slices"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
)

const (
	noFairShareDrfMultiplier = 1000
)

type QueueAttributes struct {
	UID               common_info.QueueID
	Name              string
	ParentQueue       common_info.QueueID
	ChildQueues       []common_info.QueueID
	CreationTimestamp metav1.Time
	Priority          int
	QueueResourceShare
}

func (q *QueueAttributes) Clone() *QueueAttributes {
	return &QueueAttributes{
		UID:                q.UID,
		Name:               q.Name,
		ParentQueue:        q.ParentQueue,
		ChildQueues:        slices.Clone(q.ChildQueues),
		CreationTimestamp:  q.CreationTimestamp,
		Priority:           q.Priority,
		QueueResourceShare: q.QueueResourceShare,
	}
}

func (q *QueueAttributes) IsTopQueue() bool {
	return q.ParentQueue == ""
}

type QueueResourceShare struct {
	CPU    ResourceShare
	Memory ResourceShare
	GPU    ResourceShare

	// cache
	lastDeservedShare ResourceQuantities
	lastFairShare     ResourceQuantities
}
type resourceShareMapFunc func(rs *ResourceShare) float64

func (qrs *QueueResourceShare) ResourceShare(resource ResourceName) *ResourceShare {
	switch resource {
	case CpuResource:
		return &qrs.CPU
	case MemoryResource:
		return &qrs.Memory
	case GpuResource:
		return &qrs.GPU
	}
	return nil
}

func (qrs *QueueResourceShare) GetAllocatableShare() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.GetAllocatableShare()
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) GetFairShare() ResourceQuantities {
	if qrs.lastFairShare == nil {
		f := func(rs *ResourceShare) float64 {
			return rs.FairShare
		}
		qrs.lastFairShare = qrs.buildResourceQuantities(f)
	}
	return qrs.lastFairShare
}

func (qrs *QueueResourceShare) GetAllocatedShare() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.Allocated
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) GetAllocatedNonPreemptible() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.AllocatedNotPreemptible
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) GetDeservedShare() ResourceQuantities {
	if qrs.lastDeservedShare == nil {
		f := func(rs *ResourceShare) float64 {
			return rs.Deserved
		}
		qrs.lastDeservedShare = qrs.buildResourceQuantities(f)
	}
	return qrs.lastDeservedShare
}

func (qrs *QueueResourceShare) GetMaxAllowedShare() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.MaxAllowed
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) GetRequestableShare() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.GetRequestableShare()
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) GetRequestShare() ResourceQuantities {
	f := func(rs *ResourceShare) float64 {
		return rs.GetRequestShare()
	}
	return qrs.buildResourceQuantities(f)
}

func (qrs *QueueResourceShare) buildResourceQuantities(f resourceShareMapFunc) ResourceQuantities {
	quantities := ResourceQuantities{}
	for _, resource := range AllResources {
		resourceShare := qrs.ResourceShare(resource)
		quantities[resource] = f(resourceShare)
	}
	return quantities
}

func (qrs *QueueResourceShare) GetDominantResourceShare(totalResources ResourceQuantities) float64 {
	dominantResource := 0.0

	for _, resource := range AllResources {
		var value float64

		resourceShare := qrs.ResourceShare(resource)
		allocatableShare := resourceShare.GetAllocatableShare()
		if allocatableShare == commonconstants.UnlimitedResourceQuantity {
			allocatableShare = totalResources[resource]
		}

		allocated := resourceShare.Allocated

		// Avoid dividing by zero, a resource with no allocatable gets maximum penalty.
		if allocatableShare == 0 {
			value = allocated * noFairShareDrfMultiplier
		} else {
			value = allocated / allocatableShare
		}

		dominantResource = math.Max(dominantResource, value)
	}
	return dominantResource
}

func (qrs *QueueResourceShare) AddResourceShare(resource ResourceName, amount float64) {
	resourceShare := qrs.ResourceShare(resource)
	resourceShare.FairShare += amount

	// invalidate fairshare cache
	qrs.lastFairShare = nil
}

func (qrs *QueueResourceShare) SetQuotaResources(resource ResourceName, deserved float64, maxAllowed float64,
	overQuotaWeight float64) {
	resourceShare := qrs.ResourceShare(resource)
	resourceShare.Deserved = deserved
	resourceShare.MaxAllowed = maxAllowed
	resourceShare.OverQuotaWeight = overQuotaWeight

	// invalidate cache for deserved share
	qrs.lastDeservedShare = nil
}

func (qrs *QueueResourceShare) GetResourceUsage() queue_info.QueueUsage {
	return queue_info.QueueUsage{
		commonconstants.NvidiaGpuResource: qrs.GPU.Usage,
		v1.ResourceCPU:                    qrs.CPU.Usage,
		v1.ResourceMemory:                 qrs.Memory.Usage,
	}
}

func (qrs *QueueResourceShare) SetResourceUsage(usage queue_info.QueueUsage) {
	qrs.GPU.Usage = usage[commonconstants.NvidiaGpuResource]
	qrs.CPU.Usage = usage[v1.ResourceCPU]
	qrs.Memory.Usage = usage[v1.ResourceMemory]
}
