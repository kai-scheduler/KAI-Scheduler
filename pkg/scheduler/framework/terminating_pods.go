// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// terminatingPods are the pods that were already terminating when the session opened: Releasing,
// but not evicted by this session. Their resources already count as releasing, yet they still hold
// their host ports, inter-pod affinity and DRA devices until they exit.
type terminatingPods struct {
	byNode map[string][]*pod_info.PodInfo
	// draHolders are the pods with DRA claims, whose devices a pool shared across nodes can hand out anywhere.
	draHolders []*pod_info.PodInfo
	// k8sPods are the pods as the inter-pod affinity plugin sees them.
	k8sPods                 []k8sPod
	hasRequiredAntiAffinity bool
	// hasSharedDevicePool is set when a DRA device pool is reachable from more than one node.
	hasSharedDevicePool bool
}

type k8sPod struct {
	podInfo  ksf.PodInfo
	nodeInfo *k8sframework.NodeInfo
}

func (ssn *Session) terminating() *terminatingPods {
	if ssn.terminatingPods != nil {
		return ssn.terminatingPods
	}
	t := &terminatingPods{byNode: map[string][]*pod_info.PodInfo{}}
	ssn.terminatingPods = t
	for _, node := range ssn.ClusterInfo.Nodes {
		// Terminating pods are in the node's affinity index (only this session's evictions are left out),
		// which already holds them parsed.
		affinityInfo, ok := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo)
		if !ok {
			continue
		}
		k8sNode := affinityInfo.NodeInfo
		for _, podInfo := range k8sNode.Pods {
			nodeTask := node.PodInfos[pod_info.PodKey(podInfo.GetPod())]
			if nodeTask == nil || !isTerminating(nodeTask) {
				continue
			}
			// The node holds a copy; eviction must act on the job's task.
			job := ssn.ClusterInfo.PodGroupInfos[nodeTask.Job]
			if job == nil {
				continue
			}
			task := job.GetAllPodsMap()[nodeTask.UID]
			if task == nil {
				continue
			}
			t.byNode[node.Name] = append(t.byNode[node.Name], task)
			if len(task.Pod.Spec.ResourceClaims) > 0 {
				t.draHolders = append(t.draHolders, task)
			}
			t.k8sPods = append(t.k8sPods, k8sPod{podInfo: podInfo, nodeInfo: k8sNode})
			if len(podInfo.GetRequiredAntiAffinityTerms()) > 0 {
				t.hasRequiredAntiAffinity = true
			}
		}
	}
	for _, slice := range ssn.ClusterInfo.ResourceSlices {
		if ptr.Deref(slice.Spec.NodeName, "") == "" {
			t.hasSharedDevicePool = true
		}
	}
	return t
}

func isTerminating(task *pod_info.PodInfo) bool {
	return task.Status == pod_status.Releasing && !task.IsVirtualStatus
}

// withoutTerminating is what has to be set aside to see a task's fit on a node once the terminating
// pods are gone.
type withoutTerminating struct {
	pods []*pod_info.PodInfo
	// affinity is set when removing the terminating pods can change the task's inter-pod affinity
	// result. Affinity is counted per topology domain, so it looks past the node.
	affinity bool
}

func (w withoutTerminating) changesNothing() bool {
	return len(w.pods) == 0 && !w.affinity
}

// withoutTerminatingPods picks the terminating pods to evict for the task on the node: the node's own
// pods, plus the DRA holders elsewhere when the task's devices may come from a pool shared across
// nodes. Inter-pod affinity looks past the node too, but its filter reads only the prefilter counts.
func (ssn *Session) withoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo) withoutTerminating {
	t := ssn.terminating()
	pods := t.byNode[node.Name]
	if t.hasSharedDevicePool && usesDRA(task) {
		// The node's DRA holders appear twice; the second eviction is skipped.
		pods = append(slices.Clip(pods), t.draHolders...)
	}
	return withoutTerminating{
		pods:     pods,
		affinity: len(t.k8sPods) > 0 && (t.hasRequiredAntiAffinity || hasRequiredPodAffinityTerms(task.Pod)),
	}
}

// WithoutTerminatingPods runs fn on the session as it will be once the terminating pods relevant to
// the task on the node are gone.
func (ssn *Session) WithoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo, fn func()) {
	ssn.runWithout(ssn.withoutTerminatingPods(task, node), task, fn)
}

// runWithout evicts the pods through the regular eviction path, runs fn, then rolls back.
func (ssn *Session) runWithout(without withoutTerminating, task *pod_info.PodInfo, fn func()) {
	if without.affinity {
		restore := ssn.removeFromAffinityState(task.Pod)
		defer restore()
	}
	if len(without.pods) == 0 {
		fn()
		return
	}

	// A scratch statement leaves the caller's statement and checkpoints untouched.
	stmt := ssn.Statement()
	for _, pod := range without.pods {
		if !isTerminating(pod) {
			continue
		}
		if err := stmt.Evict(pod, "", eviction_info.EvictionMetadata{}); err != nil {
			log.InfraLogger.V(6).Infof("Failed to set aside terminating pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
		}
	}
	fn()
	if err := stmt.Rollback(0); err != nil {
		log.InfraLogger.Errorf("Failed to restore terminating pods: %v", err)
	}
}

// removeFromAffinityState swaps the pod's prefilter state for a copy without the terminating pods in
// its inter-pod affinity counts, as kube-scheduler does for preemption victims. Rerunning the
// prefilter would be costlier, and a Skip it returns would stick for the rest of the session.
func (ssn *Session) removeFromAffinityState(pod *v1.Pod) (restore func()) {
	extensions := ssn.podAffinityExtensions()
	if extensions == nil {
		return func() {}
	}
	state := ssn.GetSessionStateForResource(pod.UID)
	derived := state.Clone()
	for _, terminating := range ssn.terminating().k8sPods {
		// Fails when the prefilter was skipped: the pod has no affinity counts to adjust.
		status := extensions.RemovePod(context.Background(), derived, pod, terminating.podInfo, terminating.nodeInfo)
		if !status.IsSuccess() {
			return func() {}
		}
	}
	ssn.k8sResourceStateCache.Store(pod.UID, derived)
	return func() { ssn.k8sResourceStateCache.Store(pod.UID, state) }
}

func (ssn *Session) podAffinityExtensions() ksf.PreFilterExtensions {
	if ssn.Cache == nil || ssn.InternalK8sPlugins() == nil {
		return nil
	}
	if plugin, ok := ssn.InternalK8sPlugins().PodAffinity.(ksf.PreFilterPlugin); ok {
		return plugin.PreFilterExtensions()
	}
	return nil
}

// fitsWithoutTerminatingPods runs the task's predicates on the node with the terminating pods set
// aside. Queue capacity is checked after the rollback: the evictions take the terminating pods off
// their queues, which never counted them, so the queues look emptier than they are.
func (ssn *Session) fitsWithoutTerminatingPods(
	without withoutTerminating, task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
) error {
	var err error
	ssn.runWithout(without, task, func() {
		err = ssn.PredicateFn(task, job, node)
	})
	if err != nil {
		return err
	}
	if result := ssn.IsTaskAllocationOnNodeOverCapacityFn(task, job, node); !result.IsSchedulable {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node does not have enough capacity. Reason: %s, Details: %s", result.Reason, result.Message))
	}
	return nil
}

func hasRequiredPodAffinityTerms(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil {
		return false
	}
	return (affinity.PodAffinity != nil && len(affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0) ||
		(affinity.PodAntiAffinity != nil && len(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) > 0)
}

// usesDRA reports whether the task needs DRA devices.
func usesDRA(task *pod_info.PodInfo) bool {
	return len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil
}
