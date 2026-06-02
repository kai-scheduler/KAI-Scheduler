// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeavailability

import (
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

type nodeAvailabilityPlugin struct{}

// New function returns nodeAvailabilityPlugin object
func New(_ framework.PluginArguments) framework.Plugin {
	return &nodeAvailabilityPlugin{}
}

func (pp *nodeAvailabilityPlugin) Name() string {
	return "nodeavailability"
}

func (pp *nodeAvailabilityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddNodeOrderFn(pp.nodeOrderFn)
}

func (pp *nodeAvailabilityPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	score := 0.0
	if taskAllocatable := node.IsTaskAllocatable(task); taskAllocatable {
		score = scores.Availability
	}

	log.InfraLogger.V(7).Infof("%s", &nodeOrderLog{task: task, node: node, score: score})
	return score, nil
}

func (pp *nodeAvailabilityPlugin) OnSessionClose(_ *framework.Session) {}

// nodeOrderLog defers argument lookups and formatting until the log line
// fires; when V(7) is disabled, String() is never invoked.
type nodeOrderLog struct {
	task  *pod_info.PodInfo
	node  *node_info.NodeInfo
	score float64
}

func (l *nodeOrderLog) String() string {
	return fmt.Sprintf(
		"Estimating Task: <%v/%v> Job: <%v> for node: <%s> that has <%f> idle GPUs and <%f> releasing GPUs and <%f> allocated GPUs. Score: %f",
		l.task.Namespace, l.task.Name, l.task.Job, l.node.Name,
		l.node.IdleVector.Get(resource_info.GPUIndex),
		l.node.ReleasingVector.Get(resource_info.GPUIndex),
		l.node.UsedVector.Get(resource_info.GPUIndex),
		l.score)
}
