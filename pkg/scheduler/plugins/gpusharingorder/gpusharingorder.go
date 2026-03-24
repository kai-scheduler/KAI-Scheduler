// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingorder

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

type gpuSharingOrderPlugin struct {
}

func New(_ framework.PluginArguments) framework.Plugin {
	return &gpuSharingOrderPlugin{}
}

func (g *gpuSharingOrderPlugin) Name() string {
	return "gpusharingorder"
}

func (g *gpuSharingOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddNodeOrderFn(g.nodeOrderFn)
}

func (g *gpuSharingOrderPlugin) nodeOrderFn(pod *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	score := 0.0
	for gpuGroup := range node.UsedSharedGPUsMemory {
		if !node.IsTaskFitOnGpuGroup(pod.ResReq, gpuGroup) {
			continue
		}

		// give a lower score if there's a used GPU that is not reserved yet -
		// for example, a different pod that was pipelined and waiting for pod reservation now.
		score = scores.GpuSharing
	}

	log.InfraLogger.V(7).Infof("Estimating Task: <%v/%v> Job: <%v> for node: <%s>. Score: %f",
		pod.Namespace, pod.Name, pod.Job, node.Name, score)
	return score, nil
}

func (g *gpuSharingOrderPlugin) OnSessionClose(_ *framework.Session) {}
