// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// topologySpreadScenarioValidator rejects a reclaim/preempt scenario when a pending pod's
// topologySpreadConstraints hold only because the victims have not been re-placed yet.
// When the simulation places the preemptor before a victim, PodTopologySpread, unlike
// inter-pod anti-affinity, never checks the pods already on a node against the incoming
// pod, so the victim can land next to the preemptor and break its skew. The victim is
// evicted while the preemptor stays unplaceable, and the next cycle can repeat it.
// A spread that already fails without the re-placed victims is left to the placement-time
// checks, so gang members that were placed in an earlier order are not held against each other.
func (pp *predicatesPlugin) topologySpreadScenarioValidator(
	k8sPredicates k8s_internal.SessionPredicates,
) api.ScenarioValidatorFn {
	spread, found := k8sPredicates[predicates.PodTopologySpread]
	return func(scenario api.ScenarioInfo) bool {
		if !found {
			return true
		}
		var constrained []*pod_info.PodInfo
		for _, task := range scenario.GetPreemptor().GetAllPodsMap() {
			if isSimulatedPlacement(task) && len(task.Pod.Spec.TopologySpreadConstraints) > 0 {
				constrained = append(constrained, task)
			}
		}
		if len(constrained) == 0 {
			return true
		}

		var replacedVictims []*pod_info.PodInfo
		for _, victim := range scenario.GetVictims() {
			for _, task := range victim.Tasks {
				if task.Status != pod_status.Releasing {
					replacedVictims = append(replacedVictims, task)
				}
			}
		}

		for _, task := range constrained {
			if pp.spreadHoldsWithout(task, nil, spread) || !pp.spreadHoldsWithout(task, replacedVictims, spread) {
				continue
			}
			log.InfraLogger.V(6).Do(func() {
				log.InfraLogger.Infof("Rejecting scenario: topology spread of task <%s/%s> on node <%s> "+
					"no longer holds after victim re-placement", task.Namespace, task.Name, task.NodeName)
			})
			return false
		}
		return true
	}
}

func isSimulatedPlacement(task *pod_info.PodInfo) bool {
	return task.Status == pod_status.Pipelined || task.Status == pod_status.Allocated
}

// spreadHoldsWithout evaluates the spread predicate for a task that is already placed in the session,
// with the task itself and the given other tasks taken out of their nodes' pod index.
func (pp *predicatesPlugin) spreadHoldsWithout(
	task *pod_info.PodInfo, others []*pod_info.PodInfo, spread k8s_internal.SessionPredicate,
) bool {
	node := pp.ssn.ClusterInfo.Nodes[task.NodeName]
	affinityInfo := k8sNodeAffinityInfo(node)
	if affinityInfo == nil {
		return true
	}
	for _, removed := range append([]*pod_info.PodInfo{task}, others...) {
		removedInfo := k8sNodeAffinityInfo(pp.ssn.ClusterInfo.Nodes[removed.NodeName])
		if removedInfo == nil || removedInfo.RemovePod(removed.Pod) != nil {
			continue
		}
		defer removedInfo.AddPod(removed.Pod)
	}
	return spreadHolds(task, affinityInfo, spread)
}

func k8sNodeAffinityInfo(node *node_info.NodeInfo) *cluster_info.K8sNodePodAffinityInfo {
	if node == nil {
		return nil
	}
	affinityInfo, ok := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo)
	if !ok || affinityInfo == nil || affinityInfo.NodeInfo == nil {
		return nil
	}
	return affinityInfo
}

func spreadHolds(
	task *pod_info.PodInfo, affinityInfo *cluster_info.K8sNodePodAffinityInfo, spread k8s_internal.SessionPredicate,
) bool {
	if _, status := spread.PreFilter(task.Pod); status.AsError() != nil {
		return false
	}
	fit, _, err := spread.Filter(task.Pod, affinityInfo.NodeInfo)
	return err == nil && fit
}
