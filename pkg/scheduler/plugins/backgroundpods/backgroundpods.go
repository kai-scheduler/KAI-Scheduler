// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package backgroundpods removes maintenance pods from the scheduler's view of the cluster for the
// duration of a session, so that capacity they hold is treated as available. At session close, each
// one is offered its place back; those that no longer fit are evicted for real.
package backgroundpods

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	Name = "backgroundpods"

	labelSelectorArg = "labelSelector"
	namespacesArg    = "namespaces"

	defaultLabelSelector = "kai.scheduler/background=true"

	evictionMessage = "Evicted to make room for a workload on this node"
)

type backgroundPodsPlugin struct {
	selector   labels.Selector
	namespaces map[string]bool

	// Session scoped. Rebuilt on every OnSessionOpen, cleared on OnSessionClose.
	statement     *framework.Statement
	evictedByNode map[string][]*pod_info.PodInfo
	podsAtOpen    map[string]map[common_info.PodID]bool
}

func New(arguments framework.PluginArguments) framework.Plugin {
	plugin := &backgroundPodsPlugin{}

	selectorArg := arguments.GetString(labelSelectorArg, defaultLabelSelector)
	selector, err := labels.Parse(selectorArg)
	if err != nil {
		log.InfraLogger.Errorf("Failed to parse %s %q, background pods plugin disabled: %v",
			labelSelectorArg, selectorArg, err)
		return plugin
	}
	plugin.selector = selector

	if namespaces := arguments.GetString(namespacesArg, ""); namespaces != "" {
		plugin.namespaces = map[string]bool{}
		for namespace := range strings.SplitSeq(namespaces, ",") {
			if trimmed := strings.TrimSpace(namespace); trimmed != "" {
				plugin.namespaces[trimmed] = true
			}
		}
	}

	return plugin
}

func (p *backgroundPodsPlugin) Name() string {
	return Name
}

// OnSessionOpen evicts every background pod from the in-memory snapshot. The statement is not
// committed here, so no eviction request is sent: the pods move to Releasing and the capacity they
// hold shows up in the nodes' releasing vectors for the session to plan against.
func (p *backgroundPodsPlugin) OnSessionOpen(ssn *framework.Session) {
	p.statement = nil
	p.evictedByNode = nil
	p.podsAtOpen = nil

	if p.selector == nil {
		return
	}

	statement := ssn.Statement()
	evictedByNode := map[string][]*pod_info.PodInfo{}
	podsAtOpen := map[string]map[common_info.PodID]bool{}

	for nodeName, node := range ssn.ClusterInfo.Nodes {
		// Collected before evicting: Statement.Evict mutates node.PodInfos.
		for _, podInfo := range p.backgroundPodsOnNode(ssn, node) {
			err := statement.Evict(podInfo, evictionMessage, eviction_info.EvictionMetadata{Action: Name})
			if err != nil {
				log.InfraLogger.V(3).Infof("Failed to virtually evict background pod <%s/%s>: %v",
					podInfo.Namespace, podInfo.Name, err)
				continue
			}
			evictedByNode[nodeName] = append(evictedByNode[nodeName], podInfo)
		}

		if len(evictedByNode[nodeName]) > 0 {
			podsAtOpen[nodeName] = podKeys(node)
		}
	}

	p.statement = statement
	p.evictedByNode = evictedByNode
	p.podsAtOpen = podsAtOpen

	log.InfraLogger.V(4).Infof("Background pods: virtually evicted %d pods across %d nodes",
		countPods(evictedByNode), len(evictedByNode))
}

// OnSessionClose offers every virtually evicted pod its place back. Pods that still fit are
// unevicted, and were never disturbed. The rest stay in the statement, and committing it turns
// them into real eviction requests.
//
// This relies on plugins being closed in reverse open order, so that the event handlers a statement
// operation triggers still belong to plugins that are alive.
func (p *backgroundPodsPlugin) OnSessionClose(ssn *framework.Session) {
	statement := p.statement
	evictedByNode := p.evictedByNode
	podsAtOpen := p.podsAtOpen
	p.statement = nil
	p.evictedByNode = nil
	p.podsAtOpen = nil

	if statement == nil {
		return
	}

	restored, displaced := 0, 0
	for nodeName, podInfos := range evictedByNode {
		node, found := ssn.ClusterInfo.Nodes[nodeName]
		if !found {
			continue
		}

		// Evaluated before restoring anything, so that a pod restored here does not count as an
		// arrival for the pods after it.
		received := receivedPods(node, podsAtOpen[nodeName])

		for _, podInfo := range sortedByName(podInfos) {
			if !p.canRestore(ssn, node, podInfo, received) {
				displaced++
				continue
			}
			if err := statement.Unevict(podInfo); err != nil {
				log.InfraLogger.Errorf("Failed to restore background pod <%s/%s> on node <%s>: %v",
					podInfo.Namespace, podInfo.Name, nodeName, err)
				displaced++
				continue
			}
			restored++
		}
	}

	log.InfraLogger.V(4).Infof("Background pods: restored %d, evicting %d", restored, displaced)

	if err := statement.Commit(); err != nil {
		log.InfraLogger.Errorf("Failed to commit background pod evictions: %v", err)
	}
}

// canRestore asks whether the node still has room for the pod, given everything the session
// committed there. Capacity is checked against idle plus releasing, because the pod's own resources
// are in the releasing vector and any pipelined task has already drawn against it. The plain
// IsTaskAllocatable reads idle alone, so the pod would never fit on the node it is running on.
//
// Predicates are re-run when the node received a pod this session. A workload that fit in idle
// capacity was allocated and bound while this pod was out of the node's inter-pod affinity index
// (node_info.excludedFromPodAffinity), so restoring it can co-locate two pods that a required
// anti-affinity forbids, which kubelet does not catch. Evaluating the pod against its own node is
// meaningful for the same reason: it is no longer indexed there, so it is not counted against
// itself. Nodes that received nothing cannot have gained a conflict, and skipping them keeps the
// common case free of predicate evaluation.
func (p *backgroundPodsPlugin) canRestore(
	ssn *framework.Session, node *node_info.NodeInfo, podInfo *pod_info.PodInfo, checkPredicates bool,
) bool {
	if !node.IsTaskAllocatableOnReleasingOrIdle(podInfo) {
		return false
	}

	if !checkPredicates {
		return true
	}

	job, found := ssn.ClusterInfo.PodGroupInfos[podInfo.Job]
	if !found {
		return false
	}

	// PrePredicateFn first: the k8s filters read the cycle state their prefilter populates.
	if err := ssn.PrePredicateFn(podInfo, job); err != nil {
		log.InfraLogger.V(5).Infof("Background pod <%s/%s> fails pre-predicates, not restoring: %v",
			podInfo.Namespace, podInfo.Name, err)
		return false
	}

	if err := ssn.PredicateFn(podInfo, job, node); err != nil {
		log.InfraLogger.V(5).Infof("Background pod <%s/%s> no longer fits node <%s>, not restoring: %v",
			podInfo.Namespace, podInfo.Name, node.Name, err)
		return false
	}

	return true
}

func podKeys(node *node_info.NodeInfo) map[common_info.PodID]bool {
	keys := make(map[common_info.PodID]bool, len(node.PodInfos))
	for key := range node.PodInfos {
		keys[key] = true
	}
	return keys
}

// receivedPods reports whether the node holds a pod it did not hold when the session opened.
func receivedPods(node *node_info.NodeInfo, atOpen map[common_info.PodID]bool) bool {
	for key := range node.PodInfos {
		if !atOpen[key] {
			return true
		}
	}
	return false
}

// backgroundPodsOnNode returns the session's own PodInfo for each background pod on the node.
//
// NodeInfo.PodInfos holds clones, so that a status change on a task does not disturb the node's
// accounting. Evicting a clone would flip the status on the very object RemoveTask reads, making
// the remove and the add cancel out, so the task is resolved from its PodGroup instead.
func (p *backgroundPodsPlugin) backgroundPodsOnNode(
	ssn *framework.Session, node *node_info.NodeInfo,
) []*pod_info.PodInfo {
	var candidates []*pod_info.PodInfo
	for _, nodeCopy := range node.PodInfos {
		if !p.isBackgroundPod(nodeCopy) {
			continue
		}

		job, found := ssn.ClusterInfo.PodGroupInfos[nodeCopy.Job]
		if !found {
			// Statement.Evict needs the pod's PodGroup, so pods KAI did not schedule are skipped.
			log.InfraLogger.V(5).Infof("Background pod <%s/%s> has no PodGroup in session, skipping",
				nodeCopy.Namespace, nodeCopy.Name)
			continue
		}

		podInfo, found := job.GetAllPodsMap()[nodeCopy.UID]
		if !found {
			continue
		}
		candidates = append(candidates, podInfo)
	}
	return sortedByName(candidates)
}

func (p *backgroundPodsPlugin) isBackgroundPod(podInfo *pod_info.PodInfo) bool {
	if podInfo.Pod == nil || podInfo.NodeName == "" {
		return false
	}

	// Anything not currently holding its node's resources has nothing to give back.
	if !pod_status.AllocatedStatus(podInfo.Status) {
		return false
	}

	if p.namespaces != nil && !p.namespaces[podInfo.Namespace] {
		return false
	}

	return p.selector.Matches(labels.Set(podInfo.Pod.Labels))
}

// sortedByName keeps the restore order stable, so the same session state always displaces the same
// pods. Which pod wins is not otherwise meaningful.
func sortedByName(podInfos []*pod_info.PodInfo) []*pod_info.PodInfo {
	sorted := make([]*pod_info.PodInfo, len(podInfos))
	copy(sorted, podInfos)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func countPods(byNode map[string][]*pod_info.PodInfo) int {
	total := 0
	for _, podInfos := range byNode {
		total += len(podInfos)
	}
	return total
}
