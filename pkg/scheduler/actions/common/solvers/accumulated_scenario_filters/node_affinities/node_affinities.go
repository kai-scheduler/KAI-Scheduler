// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_affinities

import (
	"context"
	"maps"
	"slices"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
)

const (
	nodeAffinitiesFilterName = "AccumulatedNodeAffinities"
)

type NodeAffinitiesFilter struct {
	allNodes           map[string]*v1.Node
	allNodeInfos       map[string]*k8sframework.NodeInfo
	feasibleNodes      map[string]*v1.Node
	processedVictims   map[common_info.PodID]bool
	nodeAffinityPlugin k8sframework.Plugin
}

func NewNodeAffinitiesFilter(
	scenario *scenario.ByNodeScenario,
	feasibleNodeInfos map[string]*node_info.NodeInfo,
	session *framework.Session,
) *NodeAffinitiesFilter {
	if scenario == nil || !preemptorHasPodsWithNodeAffinities(scenario) {
		return nil
	}

	nodeAffinitiesFilter := &NodeAffinitiesFilter{
		feasibleNodes:      make(map[string]*v1.Node, len(feasibleNodeInfos)),
		allNodes:           make(map[string]*v1.Node, len(session.ClusterInfo.Nodes)),
		allNodeInfos:       make(map[string]*k8sframework.NodeInfo, len(session.ClusterInfo.Nodes)),
		processedVictims:   make(map[common_info.PodID]bool),
		nodeAffinityPlugin: session.Cache.InternalK8sPlugins().NodeAffinity,
	}
	nodeAffinitiesFilter.initNodeMaps(feasibleNodeInfos, session)
	nodeAffinitiesFilter.updateStateWithScenario(scenario)

	return nodeAffinitiesFilter
}

func (naf *NodeAffinitiesFilter) initNodeMaps(feasibleNodeInfos map[string]*node_info.NodeInfo, session *framework.Session) {
	for name, ni := range feasibleNodeInfos {
		naf.feasibleNodes[name] = ni.Node
	}
	for name, ni := range session.ClusterInfo.Nodes {
		naf.allNodes[name] = ni.Node
		naf.allNodeInfos[name] = naf.k8sNodeInfoForNode(ni.Node)
	}
}

func preemptorHasPodsWithNodeAffinities(scenario *scenario.ByNodeScenario) bool {
	for _, task := range scenario.PendingTasks() {
		if hasRequiredNodeAffinity(task) {
			return true
		}
	}
	return false
}

func (naf *NodeAffinitiesFilter) Name() string {
	return nodeAffinitiesFilterName
}

func (naf *NodeAffinitiesFilter) Filter(scenario *scenario.ByNodeScenario) (bool, error) {
	naf.updateStateWithScenario(scenario)
	return naf.allPendingPodsHaveMatchingNodes(scenario), nil
}

func (naf *NodeAffinitiesFilter) updateStateWithScenario(scenario *scenario.ByNodeScenario) {
	for _, task := range scenario.PotentialVictimsTasks() {
		naf.updateVictimNodesFromTask(task)
	}
	for _, task := range scenario.RecordedVictimsTasks() {
		naf.updateVictimNodesFromTask(task)
	}
}

func (naf *NodeAffinitiesFilter) updateVictimNodesFromTask(task *pod_info.PodInfo) {
	if _, alreadySeenTask := naf.processedVictims[task.UID]; alreadySeenTask {
		return
	}
	naf.processedVictims[task.UID] = true
	if task.NodeName == "" {
		return
	}
	if _, ok := naf.feasibleNodes[task.NodeName]; ok {
		return
	}
	naf.feasibleNodes[task.NodeName] = naf.allNodes[task.NodeName]
}

func (naf *NodeAffinitiesFilter) allPendingPodsHaveMatchingNodes(scenario *scenario.ByNodeScenario) bool {
	for _, task := range scenario.PendingTasks() {
		if !hasRequiredNodeAffinity(task) {
			continue
		}
		if !naf.hasNodeMatchingPodInSet(task) {
			return false
		}
	}
	return true
}

func hasRequiredNodeAffinity(task *pod_info.PodInfo) bool {
	if task.Pod == nil {
		return false
	}
	if task.Pod.Spec.NodeSelector != nil {
		return true
	}
	if task.Pod.Spec.Affinity == nil || task.Pod.Spec.Affinity.NodeAffinity == nil {
		return false
	}
	return task.Pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil
}

func (naf *NodeAffinitiesFilter) hasNodeMatchingPodInSet(task *pod_info.PodInfo) bool {
	preFilterPlugin := naf.nodeAffinityPlugin.(k8s_internal.NodePreFilter)
	filterPlugin := naf.nodeAffinityPlugin.(k8s_internal.NodeFilter)

	state := k8s_internal.NewSessionState()
	preFilteredNodes, preFilterPassed := naf.preFilteredNodeNames(preFilterPlugin, state, task.Pod)
	if !preFilterPassed {
		return false
	}
	for allowedNodeName := range preFilteredNodes {
		k8sNodeInfo := naf.allNodeInfos[allowedNodeName]
		if k8sNodeInfo == nil {
			continue
		}
		status := filterPlugin.Filter(context.Background(), state, task.Pod, k8sNodeInfo)
		if status == nil || status.IsSuccess() {
			return true
		}
	}
	return false
}

func (naf *NodeAffinitiesFilter) preFilteredNodeNames(
	preFilterPlugin k8s_internal.NodePreFilter,
	state k8s_internal.SessionState,
	pod *v1.Pod,
) (sets.Set[string], bool) {
	preFilterResult, status := preFilterPlugin.PreFilter(
		context.Background(), state, pod, naf.feasibleNodeInfos())
	if status != nil && status.IsSkip() {
		// Skip means no required terms (e.g. preferred-only affinity) — all nodes remain eligible.
		return sets.New(slices.Collect(maps.Keys(naf.feasibleNodes))...), true
	}
	if status != nil && !status.IsSuccess() {
		return nil, false
	}
	if preFilterResult == nil || preFilterResult.NodeNames == nil {
		// Per the k8s scheduler framework contract, a nil PreFilterResult (or nil NodeNames within it)
		// means the plugin has no node restriction to apply — all nodes passed to PreFilter remain eligible.
		return sets.New(slices.Collect(maps.Keys(naf.feasibleNodes))...), true
	}
	return preFilterResult.NodeNames, true
}

func (naf *NodeAffinitiesFilter) feasibleNodeInfos() []ksf.NodeInfo {
	nodes := make([]ksf.NodeInfo, 0, len(naf.feasibleNodes))
	for name := range naf.feasibleNodes {
		k8sNodeInfo := naf.allNodeInfos[name]
		if k8sNodeInfo == nil {
			continue
		}
		nodes = append(nodes, k8sNodeInfo)
	}
	return nodes
}

func (naf *NodeAffinitiesFilter) k8sNodeInfoForNode(node *v1.Node) *k8sframework.NodeInfo {
	nodeInfo := k8sframework.NewNodeInfo()
	nodeInfo.SetNode(node)
	return nodeInfo
}
