// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeplacement

import (
	"math"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/scores"
)

func (pp *nodePlacementPlugin) nodeResourcePack(resourceName v1.ResourceName) api.NodeOrderFn {
	return func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		podAllocationRange := pp.podAllocatableRange[string(task.UID)]
		currentNodeNonAllocated := node.NonAllocatedResource(resourceName)

		nodeOverall := node.Allocatable.Get(resourceName)
		score := getScoreOfCurrentNode(podAllocationRange.minAllocatable, podAllocationRange.maxAllocatable,
			currentNodeNonAllocated, nodeOverall)
		log.InfraLogger.V(7).Infof("Estimating Task: <%v/%v> Job: <%v> for node: <%s> "+
			"that has <%f> non allocated %v. Score: %f",
			task.Namespace, task.Name, task.Job, node.Name, currentNodeNonAllocated, resourceName, score)
		return score, nil
	}
}

func (pp *nodePlacementPlugin) setBinpackPreOrder(task *pod_info.PodInfo, fittingNodes []*node_info.NodeInfo) error {
	jobType := jobTypeFromTask(task)
	minAllocatable, maxAllocatable := getMinMaxPerNode(resourceTypeFromJobType(jobType), fittingNodes)
	pp.podAllocatableRange[string(task.UID)] = allocationRange{
		minAllocatable: minAllocatable,
		maxAllocatable: maxAllocatable,
	}
	return nil
}

func getScoreOfCurrentNode(minAllocatable, maxAllocatable, currentNodeNonAllocated, nodeOverallResource float64) float64 {
	// Node has none of that resource
	if nodeOverallResource == 0 {
		return float64(0)
	}

	// No allocatable resource
	if maxAllocatable == 0 {
		return float64(0)
	}

	// If maxAllocatable == minAllocatable it means everyone should get the same score
	if minAllocatable == maxAllocatable {
		return float64(scores.MaxHighDensity)
	}

	// The idea of this formula is to give a higher score to a node with smaller number of available resources.
	// The highest score here is MaxHighDensity.
	return scores.MaxHighDensity * (1 - (currentNodeNonAllocated-minAllocatable)/(maxAllocatable-minAllocatable))
}

func getMinMaxPerNode(resourceName v1.ResourceName, nodes []*node_info.NodeInfo) (float64, float64) {
	var maxAllocatable float64 = 0
	var minAllocatable = math.MaxFloat64
	for _, node := range nodes {
		current := node.NonAllocatedResource(resourceName)
		// We don't want to consider nodes with none of that resource type
		if node.Allocatable.Get(resourceName) == 0 {
			continue
		}

		if current < minAllocatable {
			minAllocatable = current
		}

		if current > maxAllocatable {
			maxAllocatable = current
		}
	}

	return minAllocatable, maxAllocatable
}

func resourceTypeFromJobType(jobType string) v1.ResourceName {
	if jobType == constants.GPUResource {
		return resource_info.GPUResourceName
	}
	return v1.ResourceCPU
}
