// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

var clusterViewLogger = klog.NewKlogr()

// clusterViews are the views predicates read the session through, built on first use.
type clusterViews struct {
	once        sync.Once
	current     *currentClusterView
	future      *futureClusterView
	terminating *terminatingPods

	draOnce sync.Once
	dra     *draOverlay
}

// CurrentClusterView is the cluster as it is now, terminating pods included.
func (ssn *Session) CurrentClusterView() api.ClusterView {
	return ssn.clusterViews().current
}

// FutureClusterView is the cluster once its terminating pods are gone. It is an overlay on the
// live session, so it reflects every change the session has made so far.
func (ssn *Session) FutureClusterView() api.ClusterView {
	return ssn.clusterViews().future
}

// ClusterViewFor returns the view the task was fitted against: a pipelined task waits for the
// terminating pods to leave, so it may take what they release.
func (ssn *Session) ClusterViewFor(task *pod_info.PodInfo) api.ClusterView {
	if task.Status == pod_status.Pipelined {
		return ssn.FutureClusterView()
	}
	return ssn.CurrentClusterView()
}

func (ssn *Session) clusterViews() *clusterViews {
	ssn.views.once.Do(func() {
		ssn.views.terminating = newTerminatingPods(ssn)
		ssn.views.current = &currentClusterView{ssn: ssn}
		ssn.views.future = &futureClusterView{
			ssn:         ssn,
			terminating: ssn.views.terminating,
			podAffinity: podAffinityExtensions(ssn),
		}
	})
	return &ssn.views
}

// futureMayDiffer reports whether the future view can give the task a different predicate
// result on the node than the current view.
func (ssn *Session) futureMayDiffer(task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	terminating := ssn.clusterViews().terminating
	if terminating.isEmpty() {
		return false
	}
	if len(terminating.byNode[node.Name]) > 0 || terminating.mayChangeAffinity(task.Pod) {
		return true
	}
	return usesDRA(task) && ssn.draOverlay().sharedPool
}

func usesDRA(task *pod_info.PodInfo) bool {
	return len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil
}

// terminatingPods are the pods that were already terminating when the session opened: Releasing
// but not evicted by this session. Their resources already count as releasing.
type terminatingPods struct {
	byNode                  map[string][]*pod_info.PodInfo
	uids                    sets.Set[types.UID]
	hasRequiredAntiAffinity bool
}

func newTerminatingPods(ssn *Session) *terminatingPods {
	terminating := &terminatingPods{
		byNode: map[string][]*pod_info.PodInfo{},
		uids:   sets.New[types.UID](),
	}
	if ssn.ClusterInfo == nil {
		return terminating
	}
	for nodeName, node := range ssn.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			if task.Status != pod_status.Releasing || task.IsVirtualStatus || task.Pod == nil {
				continue
			}
			terminating.byNode[nodeName] = append(terminating.byNode[nodeName], task)
			terminating.uids.Insert(task.Pod.UID)
			if len(requiredAntiAffinityTerms(task.Pod)) > 0 {
				terminating.hasRequiredAntiAffinity = true
			}
		}
	}
	return terminating
}

func (t *terminatingPods) isEmpty() bool {
	return t.uids.Len() == 0
}

// mayChangeAffinity reports whether removing the terminating pods can change the pod's
// inter-pod affinity counts.
func (t *terminatingPods) mayChangeAffinity(pod *v1.Pod) bool {
	if t.isEmpty() {
		return false
	}
	if t.hasRequiredAntiAffinity || len(requiredAntiAffinityTerms(pod)) > 0 {
		return true
	}
	return pod.Spec.Affinity != nil && pod.Spec.Affinity.PodAffinity != nil &&
		len(pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0
}

func requiredAntiAffinityTerms(pod *v1.Pod) []v1.PodAffinityTerm {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		return nil
	}
	return pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}

func liveK8sNodeInfo(node *node_info.NodeInfo) *k8sframework.NodeInfo {
	return node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
}

type currentClusterView struct {
	ssn *Session
}

func (v *currentClusterView) K8sNodeInfo(node *node_info.NodeInfo) *k8sframework.NodeInfo {
	return liveK8sNodeInfo(node)
}

func (v *currentClusterView) CycleState(pod *v1.Pod) k8s_internal.SessionState {
	return v.ssn.GetSessionStateForResource(pod.UID)
}

func (v *currentClusterView) DRAAllocatedState(pod *v1.Pod) (*structured.AllocatedState, error) {
	return v.ssn.draOverlay().allocatedState(pod, false)
}

type futureClusterView struct {
	ssn         *Session
	terminating *terminatingPods
	// podAffinity removes a pod from the inter-pod affinity prefilter state, as kube-scheduler
	// does when it evaluates preemption victims.
	podAffinity ksf.PreFilterExtensions
}

func podAffinityExtensions(ssn *Session) ksf.PreFilterExtensions {
	if ssn.Cache == nil || ssn.InternalK8sPlugins() == nil {
		return nil
	}
	if plugin, ok := ssn.InternalK8sPlugins().PodAffinity.(ksf.PreFilterPlugin); ok {
		return plugin.PreFilterExtensions()
	}
	return nil
}

// K8sNodeInfo returns a copy of the node without its terminating pods.
func (v *futureClusterView) K8sNodeInfo(node *node_info.NodeInfo) *k8sframework.NodeInfo {
	live := liveK8sNodeInfo(node)
	terminating := v.terminating.byNode[node.Name]
	if len(terminating) == 0 {
		return live
	}
	nodeInfo := live.SnapshotConcrete()
	for _, task := range terminating {
		if err := nodeInfo.RemovePod(clusterViewLogger, task.Pod); err != nil {
			log.InfraLogger.V(4).Infof("Terminating pod %s/%s is not indexed on node %s: %v",
				task.Namespace, task.Name, node.Name, err)
		}
	}
	return nodeInfo
}

// CycleState returns a copy of the pod's prefilter state without the terminating pods of the
// whole cluster. Affinity counts are aggregated per topology domain, so a terminating pod on
// another node of the candidate's rack matters as much as one on the candidate.
func (v *futureClusterView) CycleState(pod *v1.Pod) k8s_internal.SessionState {
	state := v.ssn.GetSessionStateForResource(pod.UID)
	if v.podAffinity == nil || !v.terminating.mayChangeAffinity(pod) {
		return state
	}
	derived := state.Clone()
	for nodeName := range v.terminating.byNode {
		node, found := v.ssn.ClusterInfo.Nodes[nodeName]
		if !found {
			continue
		}
		nodeInfo := liveK8sNodeInfo(node)
		for _, podInfo := range nodeInfo.GetPods() {
			if !v.terminating.uids.Has(podInfo.GetPod().UID) {
				continue
			}
			// Fails when the prefilter was skipped: the pod has no affinity terms to adjust.
			if status := v.podAffinity.RemovePod(context.Background(), derived, pod, podInfo, nodeInfo); !status.IsSuccess() {
				return state
			}
		}
	}
	return derived
}

func (v *futureClusterView) DRAAllocatedState(pod *v1.Pod) (*structured.AllocatedState, error) {
	return v.ssn.draOverlay().allocatedState(pod, true)
}
