// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nominatednode

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/plugins/scores"
)

type nominatedNodeNamePlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &nominatedNodeNamePlugin{}
}

func (nnp *nominatedNodeNamePlugin) Name() string {
	return "nominatednode"
}

func (nnp *nominatedNodeNamePlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddNodeOrderFn(nnp.nodeOrderFn())
}

func (nnp *nominatedNodeNamePlugin) nodeOrderFn() api.NodeOrderFn {
	return func(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
		score := 0.0
		if task.Pod.Status.NominatedNodeName == node.Name {
			score = scores.NominatedNode
		}

		log.InfraLogger.V(7).Infof(
			"Estimating Task: <%v/%v> Job: <%v> for node: <%s>. Pod nomindated node name: <%s>. Score: %f",
			task.Namespace, task.Name, task.Job, node.Name, task.Pod.Status.NominatedNodeName, score)
		return score, nil
	}
}

func (nnp *nominatedNodeNamePlugin) OnSessionClose(*framework.Session) {}
