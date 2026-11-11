// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package victimreallocation

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

const PluginName = "victimreallocation"

type victimReallocationPlugin struct {
	ssn             *framework.Session
	preemptor       *podgroup_info.PodGroupInfo
	preemptorPlaced bool
}

func New(_ framework.PluginArguments) framework.Plugin {
	return &victimReallocationPlugin{}
}

func (vp *victimReallocationPlugin) Name() string {
	return PluginName
}

func (vp *victimReallocationPlugin) OnSessionOpen(ssn *framework.Session) {
	vp.ssn = ssn
	ssn.AddPreJobAllocationFn(vp.preJobAllocationFn)
	ssn.AddNodeOrderFn(vp.nodeOrderFn)
}

func (vp *victimReallocationPlugin) preJobAllocationFn(job *podgroup_info.PodGroupInfo) {
	if !hasVirtuallyEvictedTasks(job) {
		vp.preemptor = job
		vp.preemptorPlaced = false
		return
	}
	vp.preemptorPlaced = isPreemptorPlacedInSimulation(vp.ssn, vp.preemptor)
}

func (vp *victimReallocationPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	if !vp.preemptorPlaced || task.Status != pod_status.Releasing || !task.IsVirtualStatus {
		return 0, nil
	}

	score := 0.0
	if task.NodeName == node.Name {
		score = scores.VictimReallocation
	}

	log.InfraLogger.V(7).Do(func() {
		log.InfraLogger.Infof(
			"Estimating victim Task: <%v/%v> Job: <%v> for node: <%s>. Original node: <%s>. Score: %f",
			task.Namespace, task.Name, task.Job, node.Name, task.NodeName, score)
	})
	return score, nil
}

func (vp *victimReallocationPlugin) OnSessionClose(*framework.Session) {
	vp.ssn = nil
	vp.preemptor = nil
	vp.preemptorPlaced = false
}

func hasVirtuallyEvictedTasks(job *podgroup_info.PodGroupInfo) bool {
	if job == nil {
		return false
	}
	for _, task := range job.PodStatusIndex[pod_status.Releasing] {
		if task.IsVirtualStatus {
			return true
		}
	}
	return false
}

func isPreemptorPlacedInSimulation(ssn *framework.Session, preemptor *podgroup_info.PodGroupInfo) bool {
	if preemptor == nil {
		return false
	}
	if preemptor.IsResizeStandIn() {
		if ssn == nil || ssn.ClusterInfo == nil {
			return false
		}
		for _, task := range preemptor.GetAllPodsMap() {
			if !task.IsResizeStandIn {
				continue
			}
			node, found := ssn.ClusterInfo.Nodes[task.NodeName]
			return found && node.IsInPlaceResizeAllocatable(task)
		}
		return false
	}
	for _, task := range preemptor.PodStatusIndex[pod_status.Pipelined] {
		if task.IsVirtualStatus {
			return true
		}
	}
	return false
}
