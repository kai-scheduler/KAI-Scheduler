// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeplacement

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

func nodeResourceSpread(resourceName v1.ResourceName) api.NodeOrderFn {
	return func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		var resourceCount float64
		if resourceName == resource_info.GPUResourceName {
			resourceCount = float64(node.GetNumberOfGPUsInNode())
		} else {
			resourceCount = node.AllocatableVector.Get(node.VectorMap.GetIndex(resourceName))
		}

		if resourceCount == 0 {
			return 0, nil
		}

		nonAllocated := node.NonAllocatedResource(resourceName)
		score := nonAllocated / resourceCount
		log.InfraLogger.V(7).Infof("Estimating Task: <%v/%v> Job: <%v> for node: <%s> "+
			"that has <%.2f/%f> non allocated %v. Score: %f",
			task.Namespace, task.Name, task.Job, node.Name, nonAllocated, resourceCount, resourceName, score)
		return score, nil
	}
}
