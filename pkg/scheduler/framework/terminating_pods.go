// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"fmt"

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
	all    []*pod_info.PodInfo
	// k8sPods are the pods as the inter-pod affinity plugin sees them, parsed once.
	k8sPods                 []k8sPod
	hasRequiredAntiAffinity bool
	// hasSharedDevicePool is set when a DRA device pool is reachable from more than one node.
	hasSharedDevicePool bool
}

type k8sPod struct {
	podInfo  *k8sframework.PodInfo
	nodeInfo *k8sframework.NodeInfo
}

func (ssn *Session) terminating() *terminatingPods {
	if ssn.terminatingPods != nil {
		return ssn.terminatingPods
	}
	t := &terminatingPods{byNode: map[string][]*pod_info.PodInfo{}}
	ssn.terminatingPods = t
	if ssn.ClusterInfo == nil {
		return t
	}
	for _, node := range ssn.ClusterInfo.Nodes {
		for _, nodeTask := range node.PodInfos {
			if !isTerminating(nodeTask) || nodeTask.Pod == nil {
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
			t.all = append(t.all, task)
			if podInfo, err := k8sframework.NewPodInfo(task.Pod); err == nil {
				nodeInfo := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo).NodeInfo
				t.k8sPods = append(t.k8sPods, k8sPod{podInfo: podInfo, nodeInfo: nodeInfo})
			}
			if len(requiredAntiAffinityTerms(task.Pod)) > 0 {
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

// mayChangeAffinity reports whether removing the terminating pods can change the pod's inter-pod
// affinity result. Affinity is counted per topology domain, so it looks past the node.
func (t *terminatingPods) mayChangeAffinity(pod *v1.Pod) bool {
	return len(t.all) > 0 && (t.hasRequiredAntiAffinity || len(requiredAntiAffinityTerms(pod)) > 0 ||
		len(requiredAffinityTerms(pod)) > 0)
}

// terminatingPodsFor returns the terminating pods to evict for the task on the node: the node's own
// pods, or all of them when the task's DRA devices may come from a pool shared across nodes. Inter-pod
// affinity looks past the node too, but its filter reads only the prefilter counts.
func (ssn *Session) terminatingPodsFor(task *pod_info.PodInfo, node *node_info.NodeInfo) []*pod_info.PodInfo {
	t := ssn.terminating()
	if t.hasSharedDevicePool && usesDRA(task) {
		return t.all
	}
	return t.byNode[node.Name]
}

// mayFitWithoutTerminatingPods reports whether the terminating pods can change the task's predicates on the node.
func (ssn *Session) mayFitWithoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	return len(ssn.terminatingPodsFor(task, node)) > 0 || ssn.terminating().mayChangeAffinity(task.Pod)
}

// WithoutTerminatingPods runs fn on the session as it will be once the terminating pods relevant to
// the task on the node are gone: it evicts them through the regular eviction path, then rolls back.
func (ssn *Session) WithoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo, fn func()) {
	if ssn.terminating().mayChangeAffinity(task.Pod) {
		restore := ssn.removeFromAffinityState(task.Pod)
		defer restore()
	}
	pods := ssn.terminatingPodsFor(task, node)
	if len(pods) == 0 {
		fn()
		return
	}

	// A scratch statement leaves the caller's statement and checkpoints untouched.
	stmt := ssn.Statement()
	for _, pod := range pods {
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
	state := ssn.GetSessionStateForResource(pod.UID)
	if extensions == nil {
		return func() {}
	}
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

// fitsWithoutTerminatingPods runs the task's predicates on the node once the terminating pods are
// gone. Queue capacity is checked again afterwards: the evictions also take the terminating pods off
// their queues, which they keep counting against until they leave.
func (ssn *Session) fitsWithoutTerminatingPods(
	task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
) error {
	var err error
	ssn.WithoutTerminatingPods(task, node, func() {
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

func requiredAntiAffinityTerms(pod *v1.Pod) []v1.PodAffinityTerm {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		return nil
	}
	return pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}

func requiredAffinityTerms(pod *v1.Pod) []v1.PodAffinityTerm {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAffinity == nil {
		return nil
	}
	return pod.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}

// usesDRA reports whether the task needs DRA devices.
func usesDRA(task *pod_info.PodInfo) bool {
	return len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil
}
