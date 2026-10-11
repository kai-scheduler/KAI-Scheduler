// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package antiaffinity

import (
	"context"
	"fmt"
	"sync"

	k8sframework "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	kubernetesframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

type bindReadiness struct {
	nodes    []*node_info.NodeInfo
	handle   k8sframework.Handle
	features feature.Features
	states   sync.Map
}

type preparedState struct {
	revisions []uint64
	state     k8sframework.CycleState
	plugin    *interpodaffinity.InterPodAffinity
	err       error
}

// New creates a checker for the current scheduling session.
func New(nodes map[string]*node_info.NodeInfo, handle k8sframework.Handle, features feature.Features) BindReadiness {
	checker := &bindReadiness{handle: handle, features: features, nodes: make([]*node_info.NodeInfo, 0, len(nodes))}
	for _, node := range nodes {
		checker.nodes = append(checker.nodes, node)
	}
	return checker
}

func (checker *bindReadiness) Prepare(task *pod_info.PodInfo, _ []*node_info.NodeInfo) error {
	return checker.prepared(task).err
}

func (checker *bindReadiness) IsReadyForBinding(task *pod_info.PodInfo, node *node_info.NodeInfo) (bool, error) {
	prepared := checker.prepared(task)
	if prepared.err != nil {
		return false, prepared.err
	}
	if prepared.state == nil {
		return true, nil
	}
	status := prepared.plugin.Filter(context.Background(), prepared.state, task.Pod, nodeInfo(node))
	if status.IsSuccess() {
		return true, nil
	}
	if status.IsRejected() {
		return false, nil
	}
	return false, status.AsError()
}

func (checker *bindReadiness) prepared(task *pod_info.PodInfo) *preparedState {
	if cached, found := checker.states.Load(task); found {
		prepared := cached.(*preparedState)
		current := true
		for i, node := range checker.nodes {
			if prepared.revisions[i] != node.ReleasingPodsRevision {
				current = false
				break
			}
		}
		if current {
			return prepared
		}
	}
	prepared := checker.prepare(task)
	checker.states.Store(task, prepared)
	return prepared
}

func (checker *bindReadiness) prepare(task *pod_info.PodInfo) *preparedState {
	prepared := &preparedState{revisions: make([]uint64, len(checker.nodes))}
	var releasing []*node_info.NodeInfo
	for i, node := range checker.nodes {
		prepared.revisions[i] = node.ReleasingPodsRevision
		if len(node.ReleasingPods) > 0 {
			releasing = append(releasing, node)
		}
	}
	if len(releasing) == 0 {
		return prepared
	}
	required := len(k8sframework.GetPodAntiAffinityTerms(task.Pod.Spec.Affinity)) > 0
	if !required {
		for _, node := range releasing {
			for _, pod := range node.ReleasingPods {
				if pod.UID != task.UID && len(k8sframework.GetPodAntiAffinityTerms(pod.Pod.Spec.Affinity)) > 0 {
					required = true
					break
				}
			}
			if required {
				break
			}
		}
	}
	if !required {
		return prepared
	}
	snapshot := &releasingSnapshot{}
	for _, node := range releasing {
		info := nodeInfo(node)
		for _, pod := range node.ReleasingPods {
			if pod.UID == task.UID {
				continue
			}
			parsed, err := kubernetesframework.NewPodInfo(pod.Pod)
			if err != nil {
				prepared.err = err
				return prepared
			}
			info.AddPodInfo(parsed)
		}
		snapshot.nodes = append(snapshot.nodes, info)
		if len(info.GetPodsWithRequiredAntiAffinity()) > 0 {
			snapshot.requiredAntiAffinity = append(snapshot.requiredAntiAffinity, info)
		}
	}
	handle := releasingHandle{Handle: checker.handle, snapshot: snapshot}
	upstream, err := interpodaffinity.New(context.Background(), &config.InterPodAffinityArgs{}, handle, checker.features)
	if err != nil {
		prepared.err = err
		return prepared
	}
	prepared.plugin = upstream.(*interpodaffinity.InterPodAffinity)
	pod := task.Pod.DeepCopy()
	// Positive affinity is evaluated by ordinary predicates.
	if pod.Spec.Affinity != nil {
		pod.Spec.Affinity.PodAffinity = nil
	}
	state := kubernetesframework.NewCycleState()
	_, status := prepared.plugin.PreFilter(context.Background(), state, pod, snapshot.nodes)
	snapshot.nodes = nil
	snapshot.requiredAntiAffinity = nil
	if status.IsSkip() {
		return prepared
	}
	if !status.IsSuccess() {
		prepared.err = fmt.Errorf("preparing releasing anti-affinity: %s", status.Message())
		return prepared
	}
	prepared.state = state
	return prepared
}

func nodeInfo(node *node_info.NodeInfo) *kubernetesframework.NodeInfo {
	info := kubernetesframework.NewNodeInfo()
	info.SetNode(node.Node)
	return info
}
