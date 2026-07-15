// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpuspread

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const pluginName = "gpuspread"

type gpuSpreadPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &gpuSpreadPlugin{}
}

func (gsp *gpuSpreadPlugin) Name() string {
	return pluginName
}

func (gsp *gpuSpreadPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddGPUOrderFn(gpuOrderFn)
}

func (gsp *gpuSpreadPlugin) OnSessionClose(_ *framework.Session) {}

func gpuOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo, gpuIdx string) (float64, error) {
	if gpuIdx == pod_info.WholeGpuIndicator {
		return 1, nil
	}

	usedGpuPortion, err := node.GetUsedGpuPortion(gpuIdx)
	if err != nil {
		return 0, err
	}

	score := 1 - usedGpuPortion
	log.InfraLogger.V(7).Infof(
		"Estimating Task: <%v/%v> Job: <%v> for gpuIdx: <%d> on node: <%s>. Score: %f",
		task.Namespace, task.Name, task.Job, gpuIdx, node.Name, score)
	return score, nil
}
