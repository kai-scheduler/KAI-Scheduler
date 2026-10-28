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
			ssn:              ssn,
			terminating:      ssn.views.terminating,
			preFilterRemoves: preFilterRemovers(ssn),
			nodes:            map[string]futureNode{},
			states:           map[types.UID]futureState{},
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
	ssn              *Session
	terminating      *terminatingPods
	preFilterRemoves []preFilterRemover

	mutex  sync.Mutex
	nodes  map[string]futureNode
	states map[types.UID]futureState
}

type futureNode struct {
	liveGeneration int64
	nodeInfo       *k8sframework.NodeInfo
}

type futureState struct {
	sources []ksf.StateData
	state   k8s_internal.SessionState
}

// preFilterRemover removes a pod from a k8s plugin's prefilter state, as kube-scheduler does when
// it evaluates preemption victims.
type preFilterRemover struct {
	stateKey   ksf.StateKey
	extensions ksf.PreFilterExtensions
}

func preFilterRemovers(ssn *Session) []preFilterRemover {
	if ssn.Cache == nil {
		return nil
	}
	k8sPlugins := ssn.InternalK8sPlugins()
	if k8sPlugins == nil {
		return nil
	}
	var removers []preFilterRemover
	for _, plugin := range []ksf.Plugin{k8sPlugins.NodePorts, k8sPlugins.TaintToleration,
		k8sPlugins.NodeAffinity, k8sPlugins.PodAffinity, k8sPlugins.VolumeBinding} {
		preFilter, ok := plugin.(ksf.PreFilterPlugin)
		if !ok || preFilter.PreFilterExtensions() == nil {
			continue
		}
		// In-tree plugins store their prefilter state under "PreFilter" + their name.
		removers = append(removers, preFilterRemover{
			stateKey:   ksf.StateKey("PreFilter" + plugin.Name()),
			extensions: preFilter.PreFilterExtensions(),
		})
	}
	return removers
}

// K8sNodeInfo returns the node without its terminating pods. The copy is kept until the session
// changes the node's pods.
func (v *futureClusterView) K8sNodeInfo(node *node_info.NodeInfo) *k8sframework.NodeInfo {
	live := liveK8sNodeInfo(node)
	terminating := v.terminating.byNode[node.Name]
	if len(terminating) == 0 {
		return live
	}

	v.mutex.Lock()
	defer v.mutex.Unlock()
	if cached, found := v.nodes[node.Name]; found && cached.liveGeneration == live.Generation {
		return cached.nodeInfo
	}
	nodeInfo := live.SnapshotConcrete()
	for _, task := range terminating {
		if err := nodeInfo.RemovePod(clusterViewLogger, task.Pod); err != nil {
			log.InfraLogger.V(4).Infof("Terminating pod %s/%s is not indexed on node %s: %v",
				task.Namespace, task.Name, node.Name, err)
		}
	}
	v.nodes[node.Name] = futureNode{liveGeneration: live.Generation, nodeInfo: nodeInfo}
	return nodeInfo
}

// CycleState returns the pod's prefilter state without the terminating pods of the whole cluster.
// Affinity counts are aggregated per topology domain, so a terminating pod on another node of the
// candidate's rack matters as much as one on the candidate. The adjusted data is derived once per
// prefilter run of the pod; everything else is read from the live state.
func (v *futureClusterView) CycleState(pod *v1.Pod) k8s_internal.SessionState {
	state := v.ssn.GetSessionStateForResource(pod.UID)
	if !v.terminating.mayChangeAffinity(pod) || len(v.preFilterRemoves) == 0 {
		return state
	}
	sources := make([]ksf.StateData, len(v.preFilterRemoves))
	hasSource := false
	for i, remover := range v.preFilterRemoves {
		if data, err := state.Read(remover.stateKey); err == nil {
			sources[i] = data
			hasSource = true
		}
	}
	if !hasSource {
		return state
	}

	v.mutex.Lock()
	defer v.mutex.Unlock()
	if cached, found := v.states[pod.UID]; found && sameStateData(cached.sources, sources) {
		return cached.state
	}
	overrides, err := v.withoutTerminatingPods(pod, state, sources)
	if err != nil {
		log.InfraLogger.V(4).Infof("Failed to remove terminating pods from the prefilter state of %s/%s: %v",
			pod.Namespace, pod.Name, err)
		return state
	}
	overlay := &overlayCycleState{CycleState: state, overrides: overrides}
	v.states[pod.UID] = futureState{sources: sources, state: overlay}
	return overlay
}

// withoutTerminatingPods returns the prefilter data of the plugins with extensions, with the
// terminating pods removed.
func (v *futureClusterView) withoutTerminatingPods(
	pod *v1.Pod, state k8s_internal.SessionState, sources []ksf.StateData,
) (map[ksf.StateKey]ksf.StateData, error) {
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
			for i, remover := range v.preFilterRemoves {
				if sources[i] == nil {
					continue
				}
				status := remover.extensions.RemovePod(context.Background(), derived, pod, podInfo, nodeInfo)
				if !status.IsSuccess() {
					return nil, status.AsError()
				}
			}
		}
	}
	overrides := map[ksf.StateKey]ksf.StateData{}
	for i, remover := range v.preFilterRemoves {
		if sources[i] == nil {
			continue
		}
		data, err := derived.Read(remover.stateKey)
		if err != nil {
			return nil, err
		}
		overrides[remover.stateKey] = data
	}
	return overrides, nil
}

// overlayCycleState reads the overridden prefilter data, and everything else from the live state.
type overlayCycleState struct {
	ksf.CycleState
	overrides map[ksf.StateKey]ksf.StateData
}

func (s *overlayCycleState) Read(key ksf.StateKey) (ksf.StateData, error) {
	if data, found := s.overrides[key]; found {
		return data, nil
	}
	return s.CycleState.Read(key)
}

func sameStateData(a, b []ksf.StateData) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (v *futureClusterView) DRAAllocatedState(pod *v1.Pod) (*structured.AllocatedState, error) {
	return v.ssn.draOverlay().allocatedState(pod, true)
}
