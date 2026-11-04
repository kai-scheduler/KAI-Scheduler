// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	ksf "k8s.io/kube-scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/api/utilities/resources"
)

// futurePlugins are the plugins opened on the future copy: the ones whose predicates read pods on
// nodes or DRA devices. NUMA mutates node state on open, so it is left out.
var futurePlugins = map[string]bool{"predicates": true, "dynamicresources": true}

// futureSession is a disposable copy of the session's cluster without the pods that are releasing:
// those that were terminating when the session opened and those this session evicted. Predicates
// run on the copy, so the live session is never changed by the check. The copy follows the live
// session's allocate and deallocate events: the next check re-copies the nodes they touched, or
// rebuilds the whole copy when DRA claims changed.
type futureSession struct {
	ssn        *Session
	lister     *cache.K8sClusterPodAffinityInfo
	k8sPlugins *k8splugins.K8sPlugins
	dirtyNodes map[string]bool
	rebuild    bool
	// version changes whenever the copy does; the prefilter result is kept per task and version.
	version         int
	prefilteredTask common_info.PodID
	prefilterResult error
	prefilterOf     int

	terminatingByNode       map[string]bool
	hasRequiredAntiAffinity bool
	hasSharedDevicePool     bool
}

type futureCache struct {
	cache.Cache
	lister     ksf.NodeInfoLister
	k8sPlugins *k8splugins.K8sPlugins
}

func (c *futureCache) SnapshotSharedLister() ksf.NodeInfoLister   { return c.lister }
func (c *futureCache) InternalK8sPlugins() *k8splugins.K8sPlugins { return c.k8sPlugins }

// future returns the session's future state, or nil when no pod was terminating at session open.
func (ssn *Session) future() *futureSession {
	if ssn.futureState == nil {
		ssn.futureState = ssn.newFutureSession()
	}
	if len(ssn.futureState.terminatingByNode) == 0 {
		return nil
	}
	return ssn.futureState
}

func (ssn *Session) newFutureSession() *futureSession {
	f := &futureSession{terminatingByNode: map[string]bool{}, dirtyNodes: map[string]bool{}, rebuild: true}
	for _, node := range ssn.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			if !isTerminating(task) {
				continue
			}
			f.terminatingByNode[node.Name] = true
			if len(requiredAntiAffinityTerms(task.Pod)) > 0 {
				f.hasRequiredAntiAffinity = true
			}
		}
	}
	if len(f.terminatingByNode) == 0 {
		return f
	}
	for _, slice := range ssn.ClusterInfo.ResourceSlices {
		if slice.Spec.NodeName == nil || *slice.Spec.NodeName == "" {
			f.hasSharedDevicePool = true
		}
	}
	f.lister = cache.NewK8sClusterPodAffinityInfo()
	if livePlugins := ssn.InternalK8sPlugins(); livePlugins != nil {
		f.k8sPlugins = livePlugins.WithNodeInfoLister(f.lister)
	}
	markChanged := func(event *Event) {
		if UsesDRA(event.Task) {
			f.rebuild = true
		}
		f.dirtyNodes[event.Task.NodeName] = true
	}
	ssn.AddEventHandler(&EventHandler{AllocateFunc: markChanged, DeallocateFunc: markChanged})
	return f
}

// session returns the copy, brought up to date with the live session.
func (f *futureSession) session(live *Session) *Session {
	if !f.rebuild {
		if len(f.dirtyNodes) > 0 {
			f.refreshNodes(live)
			f.version++
		}
		return f.ssn
	}
	clear(f.dirtyNodes)
	f.version++
	*f.lister = *cache.NewK8sClusterPodAffinityInfo()
	clusterInfo := *live.ClusterInfo
	clusterInfo.Nodes = make(map[string]*node_info.NodeInfo, len(live.ClusterInfo.Nodes))
	for name, node := range live.ClusterInfo.Nodes {
		clusterInfo.Nodes[name] = futureNode(node, f.lister)
	}
	clusterInfo.ResourceClaims = futureClaims(live)

	f.ssn = &Session{
		ID:                    live.ID + "-future",
		Cache:                 &futureCache{Cache: live.Cache, lister: f.lister, k8sPlugins: f.k8sPlugins},
		ClusterInfo:           &clusterInfo,
		Config:                live.Config,
		SchedulerParams:       live.SchedulerParams,
		plugins:               map[string]Plugin{},
		k8sResourceStateCache: sync.Map{},
	}
	if live.Config != nil {
		for _, tier := range live.Config.Tiers {
			for _, option := range tier.Plugins {
				if !futurePlugins[option.Name] {
					continue
				}
				if builder, found := GetPluginBuilder(option.Name); found {
					plugin := builder(option.Arguments)
					f.ssn.plugins[plugin.Name()] = plugin
					plugin.OnSessionOpen(f.ssn)
				}
			}
		}
	}
	f.rebuild = false
	return f.ssn
}

// refreshNodes re-copies the nodes the live session changed and re-indexes the others as they are.
func (f *futureSession) refreshNodes(live *Session) {
	*f.lister = *cache.NewK8sClusterPodAffinityInfo()
	nodes := f.ssn.ClusterInfo.Nodes
	for name, liveNode := range live.ClusterInfo.Nodes {
		if f.dirtyNodes[name] {
			nodes[name] = futureNode(liveNode, f.lister)
			continue
		}
		affinityInfo := nodes[name].PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo)
		f.lister.AddNode(name, affinityInfo.NodeInfo)
		f.lister.UpdateNodeAffinity(affinityInfo)
	}
	clear(f.dirtyNodes)
}

// futureNode shares the live node's resource accounting, which already treats releasing resources
// as free, and indexes for k8s plugins only the pods that are not releasing.
func futureNode(live *node_info.NodeInfo, lister *cache.K8sClusterPodAffinityInfo) *node_info.NodeInfo {
	node := *live
	node.PodAffinityInfo = cluster_info.NewK8sNodePodAffinityInfo(live.Node, lister)
	node.PodInfos = make(map[common_info.PodID]*pod_info.PodInfo, len(live.PodInfos))
	for key, task := range live.PodInfos {
		if task.Status == pod_status.Releasing {
			continue
		}
		node.PodInfos[key] = task
		node.PodAffinityInfo.AddPod(task.Pod)
	}
	return &node
}

// futureClaims returns the live claims with the terminating pods' reservations dropped. Pods this
// session evicted already had their claims deallocated by the DRA plugin.
func futureClaims(live *Session) []*resourceapi.ResourceClaim {
	claims := live.ClusterInfo.ResourceClaims
	if plugins := live.InternalK8sPlugins(); plugins != nil && plugins.SessionDRAManager != nil {
		if current, err := plugins.SessionDRAManager.ResourceClaims().List(); err == nil {
			claims = current
		}
	}
	releasing := map[string]*v1.Pod{}
	for _, node := range live.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			if isTerminating(task) && len(task.Pod.Spec.ResourceClaims) > 0 {
				releasing[string(task.Pod.UID)] = task.Pod
			}
		}
	}
	if len(releasing) == 0 {
		return claims
	}
	result := make([]*resourceapi.ResourceClaim, 0, len(claims))
	for _, claim := range claims {
		var released *resourceapi.ResourceClaim
		for _, consumer := range claim.Status.ReservedFor {
			pod, found := releasing[string(consumer.UID)]
			if !found {
				continue
			}
			if released == nil {
				released = claim.DeepCopy()
			}
			resources.RemoveReservedFor(released, pod)
			if len(released.Status.ReservedFor) == 0 && !k8s_utils.ClaimOutlivesPod(released, pod) {
				released.Status.Allocation = nil
			}
		}
		if released != nil {
			claim = released
		}
		result = append(result, claim)
	}
	return result
}

// fitsInFuture reports whether the task passes its predicates on the node once the releasing pods
// are gone. Queue capacity is checked on the live session: terminating pods count against their
// queue until they leave.
func (ssn *Session) fitsInFuture(
	task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
) error {
	f := ssn.future()
	if f == nil || !f.canChange(task, node) {
		return fmt.Errorf("no terminating pods affect task <%s/%s> on node <%s>",
			task.Namespace, task.Name, node.Name)
	}
	future := f.session(ssn)
	futureNode := future.ClusterInfo.Nodes[node.Name]
	if err := f.prePredicate(future, task, job); err != nil {
		return err
	}
	if err := future.PredicateFn(task, job, futureNode); err != nil {
		return err
	}
	if result := ssn.IsTaskAllocationOnNodeOverCapacityFn(task, job, node); !result.IsSchedulable {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node does not have enough capacity. Reason: %s, Details: %s", result.Reason, result.Message))
	}
	return nil
}

// prePredicate runs the task's prefilter on the copy once per version of the copy, not per node.
func (f *futureSession) prePredicate(future *Session, task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) error {
	if f.prefilteredTask != task.UID || f.prefilterOf != f.version {
		f.prefilterResult = future.PrePredicateFn(task, job)
		f.prefilteredTask, f.prefilterOf = task.UID, f.version
	}
	return f.prefilterResult
}

// canChange reports whether removing the terminating pods can change the task's predicates on the
// node: the node hosts one, or pod (anti-)affinity or a DRA device pool spanning nodes is involved.
func (f *futureSession) canChange(task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	return f.terminatingByNode[node.Name] || f.hasRequiredAntiAffinity || hasPodAffinityTerms(task.Pod) ||
		(f.hasSharedDevicePool && UsesDRA(task))
}

// HasTerminatingPods reports whether pods were terminating when the session opened.
func (ssn *Session) HasTerminatingPods() bool {
	return ssn.future() != nil
}

// FutureDRAManager returns the DRA state without the releasing pods' devices, for a task pipelined
// onto them. It is nil when no pod was terminating at session open.
func (ssn *Session) FutureDRAManager() ksf.SharedDRAManager {
	f := ssn.future()
	if f == nil || f.k8sPlugins == nil {
		return nil
	}
	f.session(ssn)
	return f.k8sPlugins.SessionDRAManager
}

func isTerminating(task *pod_info.PodInfo) bool {
	return task.Status == pod_status.Releasing && !task.IsVirtualStatus
}

func hasPodAffinityTerms(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil {
		return false
	}
	return (affinity.PodAffinity != nil && len(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0) ||
		len(requiredAntiAffinityTerms(pod)) > 0
}

func requiredAntiAffinityTerms(pod *v1.Pod) []v1.PodAffinityTerm {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		return nil
	}
	return pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}

// UsesDRA reports whether the task needs DRA devices.
func UsesDRA(task *pod_info.PodInfo) bool {
	return len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil
}

func logFutureFit(task *pod_info.PodInfo, node *node_info.NodeInfo) {
	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("Task <%s/%s> fits node <%s> once the terminating pods are gone",
			task.Namespace, task.Name, node.Name)
	})
}
