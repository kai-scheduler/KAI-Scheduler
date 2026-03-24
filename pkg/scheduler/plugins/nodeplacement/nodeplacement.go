// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeplacement

import (
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type allocationRange struct {
	minAllocatable, maxAllocatable float64
}

type nodePlacementPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.PluginArguments
	gpuPreOrderFn   api.NodePreOrderFn
	cpuPreOrderFn   api.NodePreOrderFn
	gpuTaskScoreFn  api.NodeOrderFn
	cpuTaskScoreFn  api.NodeOrderFn

	podAllocatableRange map[string]allocationRange
}

// New function returns nodePlacementPlugin object
func New(arguments framework.PluginArguments) framework.Plugin {
	// Create a mutable copy to set defaults
	args := make(framework.PluginArguments)
	for k, v := range arguments {
		args[k] = v
	}

	for _, jobType := range []string{constants.GPUResource, constants.CPUResource} {
		if _, found := args[jobType]; !found {
			args[jobType] = constants.BinpackStrategy
		}
	}

	return &nodePlacementPlugin{pluginArguments: args}
}

func (pp *nodePlacementPlugin) Name() string {
	return "nodeplacement"
}

func (pp *nodePlacementPlugin) OnSessionOpen(ssn *framework.Session) {
	pp.podAllocatableRange = make(map[string]allocationRange)

	// pack tasks by default
	pp.gpuTaskScoreFn = pp.nodeResourcePack(resource_info.GPUResourceName)
	pp.gpuPreOrderFn = pp.setBinpackPreOrder
	if pp.pluginArguments[constants.GPUResource] == constants.SpreadStrategy {
		pp.gpuPreOrderFn = noopPreOrderFn
		pp.gpuTaskScoreFn = nodeResourceSpread(resource_info.GPUResourceName)
	}

	pp.cpuTaskScoreFn = pp.nodeResourcePack(v1.ResourceCPU)
	pp.cpuPreOrderFn = pp.setBinpackPreOrder
	if pp.pluginArguments[constants.CPUResource] == constants.SpreadStrategy {
		pp.cpuPreOrderFn = noopPreOrderFn
		pp.cpuTaskScoreFn = nodeResourceSpread(v1.ResourceCPU)
	}

	ssn.AddNodePreOrderFn(pp.nodePreOrderFn)
	ssn.AddNodeOrderFn(pp.nodeOrderFn)
}

func (pp *nodePlacementPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	if task != nil && task.IsCPUOnlyRequest() {
		return pp.cpuTaskScoreFn(task, node)
	}
	return pp.gpuTaskScoreFn(task, node)
}

func (pp *nodePlacementPlugin) nodePreOrderFn(task *pod_info.PodInfo, fittingNodes []*node_info.NodeInfo) error {
	if task != nil && task.IsCPUOnlyRequest() {
		return pp.cpuPreOrderFn(task, fittingNodes)
	}
	return pp.gpuPreOrderFn(task, fittingNodes)
}

func noopPreOrderFn(_ *pod_info.PodInfo, _ []*node_info.NodeInfo) error {
	return nil
}

func jobTypeFromTask(task *pod_info.PodInfo) string {
	jobType := constants.GPUResource
	if task != nil && task.IsCPUOnlyRequest() {
		jobType = constants.CPUResource
	}
	return jobType
}

func (pp *nodePlacementPlugin) OnSessionClose(_ *framework.Session) {}
