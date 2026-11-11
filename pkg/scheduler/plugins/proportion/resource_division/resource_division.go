// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_division

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/fairshare"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
	rs "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/proportion/resource_share"
)

func SetResourcesShare(totalResource rs.ResourceQuantities, kValue float64, queues map[common_info.QueueID]*rs.QueueAttributes) {
	DivideResources(totalResource, kValue, queues)
	reportDivisionResult(queues)
}

// DivideResources sets the queues' fair shares as SetResourcesShare does, without logging the result
// or updating the fair-share metrics.
func DivideResources(totalResource rs.ResourceQuantities, kValue float64, queues map[common_info.QueueID]*rs.QueueAttributes) {
	for _, resource := range rs.AllResources {
		setResourceShare(totalResource[resource], kValue, resource, queues)
	}
}

func setResourceShare(totalAmount, kValue float64, resourceName rs.ResourceName, queues map[common_info.QueueID]*rs.QueueAttributes) float64 {
	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("About to start calculating %v fairShare, totalAmount: <%v>", resourceName, totalAmount)
	})
	fq, attributes := toFairshareQueues(queues, resourceName)
	remaining := fairshare.Divide(totalAmount, kValue, fq)
	storeFairShare(fq, attributes, resourceName)
	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("%v resource remaining after division: %v", resourceName, remaining)
	})
	return remaining
}

func toFairshareQueues(queues map[common_info.QueueID]*rs.QueueAttributes, resourceName rs.ResourceName,
) ([]*fairshare.Queue, []*rs.QueueAttributes) {
	fq := make([]*fairshare.Queue, 0, len(queues))
	attributes := make([]*rs.QueueAttributes, 0, len(queues))
	for _, queue := range queues {
		attributes = append(attributes, queue)
		share := queue.ResourceShare(resourceName)
		fq = append(fq, &fairshare.Queue{
			ID:                string(queue.UID),
			Priority:          queue.Priority,
			CreationTimestamp: queue.CreationTimestamp.Time,
			Deserved:          share.Deserved,
			MaxAllowed:        share.MaxAllowed,
			OverQuotaWeight:   share.OverQuotaWeight,
			Request:           share.Request,
			Usage:             share.GetUsage(),
			FairShare:         share.FairShare,
		})
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("calculating %v resource fair share for %v: deserved: %v, maxAllowed: %v, "+
				"overQuotaWeight: %v, request: %v, usage: %v, fairShare: %v", resourceName, queue.Name,
				share.Deserved, share.MaxAllowed, share.OverQuotaWeight, share.Request, share.GetUsage(), share.FairShare)
		})
	}
	return fq, attributes
}

// storeFairShare goes through SetFairShare so the queues' cached fair share is invalidated.
func storeFairShare(fq []*fairshare.Queue, attributes []*rs.QueueAttributes, resourceName rs.ResourceName) {
	for i, q := range fq {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("%v resource fair share for %v: %v (was %v)", resourceName, attributes[i].Name,
				q.FairShare, attributes[i].ResourceShare(resourceName).FairShare)
		})
		attributes[i].SetFairShare(resourceName, q.FairShare)
	}
}

func reportDivisionResult(queues map[common_info.QueueID]*rs.QueueAttributes) {
	for _, queue := range queues {
		cpuResourceShare := queue.ResourceShare(rs.CpuResource)
		memoryResourceShare := queue.ResourceShare(rs.MemoryResource)
		gpuResourceShare := queue.ResourceShare(rs.GpuResource)

		metrics.UpdateQueueFairShare(
			queue.Name,
			string(queue.UID),
			queue.DisplayName,
			cpuResourceShare.FairShare/resource_info.MilliCPUToCores,
			memoryResourceShare.FairShare/resource_info.MemoryToGB,
			gpuResourceShare.FairShare,
		)

		metrics.UpdateQueueUsage(
			queue.Name,
			string(queue.UID),
			queue.DisplayName,
			cpuResourceShare.GetUsage(),
			memoryResourceShare.GetUsage(),
			gpuResourceShare.GetUsage(),
		)

		log.InfraLogger.V(3).Infof("Resource division result for queue <%v>: "+
			"Queue Priority: <%d>, "+
			"GPU: deserved: <%v>, requested: <%v>, maxAllowed: <%v>, allocated: <%v>, historicalUsage: <%v>, fairShare: <%v> "+
			"CPU (cores): deserved: <%v>, requested: <%v>, maxAllowed: <%v>, allocated: <%v>, historicalUsage: <%v>, fairShare: <%v> "+
			"memory (GB): deserved: <%v>, requested: <%v>, maxAllowed: <%v>, allocated: <%v>, historicalUsage: <%v>, fairShare: <%v> ",
			queue.Name,
			queue.Priority,
			resource_info.HumanizeResource(gpuResourceShare.Deserved, 1),
			resource_info.HumanizeResource(gpuResourceShare.GetRequestableShare(), 1),
			resource_info.HumanizeResource(gpuResourceShare.MaxAllowed, 1),
			resource_info.HumanizeResource(gpuResourceShare.Allocated, 1),
			resource_info.HumanizeResource(gpuResourceShare.GetUsage(), 1),
			resource_info.HumanizeResource(gpuResourceShare.FairShare, 1),
			resource_info.HumanizeResource(cpuResourceShare.Deserved, resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(cpuResourceShare.GetRequestableShare(), resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(cpuResourceShare.MaxAllowed, resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(cpuResourceShare.Allocated, resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(cpuResourceShare.GetUsage(), resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(cpuResourceShare.FairShare, resource_info.MilliCPUToCores),
			resource_info.HumanizeResource(memoryResourceShare.Deserved, resource_info.MemoryToGB),
			resource_info.HumanizeResource(memoryResourceShare.GetRequestableShare(), resource_info.MemoryToGB),
			resource_info.HumanizeResource(memoryResourceShare.MaxAllowed, resource_info.MemoryToGB),
			resource_info.HumanizeResource(memoryResourceShare.Allocated, resource_info.MemoryToGB),
			resource_info.HumanizeResource(memoryResourceShare.GetUsage(), resource_info.MemoryToGB),
			resource_info.HumanizeResource(memoryResourceShare.FairShare, resource_info.MemoryToGB))
	}
}
