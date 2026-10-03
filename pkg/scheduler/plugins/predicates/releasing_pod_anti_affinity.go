// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sframework "k8s.io/kube-scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type releasingTask struct {
	task *pod_info.PodInfo
	node *node_info.NodeInfo
}

func (pp *predicatesPlugin) initializeReleasingTasks() {
	pp.releasingTasks = make(map[common_info.PodID]releasingTask)
	pp.releasingTasksWithAntiAffinity = make(map[common_info.PodID]releasingTask)
	for _, node := range pp.ssn.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			pp.addReleasingTask(task, node)
		}
	}
	pp.ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   pp.updateReleasingTask,
		DeallocateFunc: pp.updateReleasingTask,
	})
}

func (pp *predicatesPlugin) addReleasingTask(task *pod_info.PodInfo, node *node_info.NodeInfo) {
	if task.Status != pod_status.Releasing {
		return
	}
	entry := releasingTask{task: task, node: node}
	pp.releasingTasks[task.UID] = entry
	if len(k8sframework.GetPodAntiAffinityTerms(task.Pod.Spec.Affinity)) > 0 {
		pp.releasingTasksWithAntiAffinity[task.UID] = entry
	}
}

func (pp *predicatesPlugin) updateReleasingTask(event *framework.Event) {
	previous, wasReleasing := pp.releasingTasks[event.Task.UID]
	delete(pp.releasingTasks, event.Task.UID)
	delete(pp.releasingTasksWithAntiAffinity, event.Task.UID)
	key := pod_info.PodKey(event.Task.Pod)
	if node := pp.ssn.ClusterInfo.Nodes[event.Task.NodeName]; node != nil {
		if task := node.PodInfos[key]; task != nil {
			pp.addReleasingTask(task, node)
		}
	}
	// A relocation can leave a releasing clone on the original node.
	if wasReleasing && previous.node.Name != event.Task.NodeName {
		if task := previous.node.PodInfos[key]; task != nil {
			pp.addReleasingTask(task, previous.node)
		}
	}
}

func (pp *predicatesPlugin) bindReady(task *pod_info.PodInfo, node *node_info.NodeInfo) (bool, error) {
	if len(pp.releasingTasks) == 0 {
		return true, nil
	}
	incomingRules := k8sframework.GetPodAntiAffinityTerms(task.Pod.Spec.Affinity)
	candidates := pp.releasingTasksWithAntiAffinity
	if len(incomingRules) > 0 {
		candidates = pp.releasingTasks
	}
	if len(candidates) == 0 {
		return true, nil
	}
	incomingTerms, err := k8sframework.GetAffinityTerms(task.Pod, incomingRules)
	if err != nil {
		return false, err
	}
	for _, entry := range candidates {
		existingTask, existingNode := entry.task, entry.node
		if existingTask.UID == task.UID {
			continue
		}
		conflict, err := pp.releasingAntiAffinityMatches(incomingTerms, existingTask.Pod, node.Node, existingNode.Node)
		if err != nil || conflict {
			return false, err
		}
		existingTerms, err := k8sframework.GetAffinityTerms(existingTask.Pod,
			k8sframework.GetPodAntiAffinityTerms(existingTask.Pod.Spec.Affinity))
		if err != nil {
			return false, err
		}
		conflict, err = pp.releasingAntiAffinityMatches(existingTerms, task.Pod, node.Node, existingNode.Node)
		if err != nil || conflict {
			return false, err
		}
	}
	return true, nil
}

func (pp *predicatesPlugin) releasingAntiAffinityMatches(terms []k8sframework.AffinityTerm, pod *v1.Pod,
	targetNode, existingNode *v1.Node) (bool, error) {
	for _, term := range terms {
		targetValue, targetHasKey := targetNode.Labels[term.TopologyKey]
		existingValue, existingHasKey := existingNode.Labels[term.TopologyKey]
		if !targetHasKey || !existingHasKey || targetValue != existingValue || !term.Selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		var namespaceLabels labels.Set
		if !term.Namespaces.Has(pod.Namespace) && term.NamespaceSelector != labels.Nothing() && !term.NamespaceSelector.Empty() {
			namespace, err := pp.ssn.Cache.KubeInformerFactory().Core().V1().Namespaces().Lister().Get(pod.Namespace)
			if err != nil {
				return false, err
			}
			namespaceLabels = labels.Set(namespace.Labels)
		}
		if term.Matches(pod, namespaceLabels) {
			return true, nil
		}
	}
	return false, nil
}
