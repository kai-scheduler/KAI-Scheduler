// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
)

func FeasibleNodesForJob(allNodes []*node_info.NodeInfo, job *podgroup_info.PodGroupInfo) []*node_info.NodeInfo {
	for _, task := range job.GetAllPodsMap() {
		if !task.IsRequireAnyKindOfGPU() {
			return allNodes
		}
	}
	var nodes []*node_info.NodeInfo
	for _, node := range allNodes {
		idleGPUs, _ := node.GetSumOfIdleGPUs()
		releasingGPUs, _ := node.GetSumOfReleasingGPUs()
		if idleGPUs > 0 || releasingGPUs > 0 {
			nodes = append(nodes, node)
		}
	}
	return nodes
}
