// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpupack

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const pluginName = "gpupack"

type gpuPackPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &gpuPackPlugin{}
}

func (gpp *gpuPackPlugin) Name() string {
	return pluginName
}

func (gpp *gpuPackPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddGPUOrderFn(gpuOrderFn)
}

func (gpp *gpuPackPlugin) OnSessionClose(_ *framework.Session) {}

func gpuOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo, gpuIdx string) (float64, error) {
	if gpuIdx == pod_info.WholeGpuIndicator {
		return 0, nil
	}

	usedGpuPortion, err := node.GetUsedGpuPortion(gpuIdx)
	if err != nil {
		return 0, err
	}

	score := usedGpuPortion
	log.InfraLogger.V(7).Infof(
		"Estimating Task: <%v/%v> Job: <%v> for gpuIdx: <%s> on node: <%s>. Score: %f",
		task.Namespace, task.Name, task.Job, gpuIdx, node.Name, score)
	return score, nil
}
