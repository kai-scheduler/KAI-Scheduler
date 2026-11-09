// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"slices"

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
	draHolders              []*pod_info.PodInfo
	k8sPods                 []k8sPod
	hasRequiredAntiAffinity bool
	hasSharedDevicePool     bool
}

func (ssn *Session) terminating() *terminatingPods {
	if ssn.terminatingPods != nil {
		return ssn.terminatingPods
	}
	t := &terminatingPods{byNode: map[string][]*pod_info.PodInfo{}}
	ssn.terminatingPods = t
	for _, node := range ssn.ClusterInfo.Nodes {
		affinityInfo, ok := node.PodAffinityInfo.(*cluster_info.K8sNodePodAffinityInfo)
		if !ok || affinityInfo == nil || affinityInfo.NodeInfo == nil {
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
			break
		}
	}
	return t
}

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
	// Queue usage never counted the terminating pods, yet evicting them subtracts them, so the capacity
	// check inside the predicates passes too easily. Recheck it against the restored usage.
	if result := ssn.IsTaskAllocationOnNodeOverCapacityFn(task, job, node); !result.IsSchedulable {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node does not have enough capacity. Reason: %s, Details: %s", result.Reason, result.Message))
	}
	return nil
}

func (ssn *Session) terminatingPodsMatter(task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	return len(ssn.terminatingPodsFor(task, node)) > 0 || ssn.terminatingPodsMatterForAffinity(task.Pod)
}

// WithoutTerminatingPods runs fn on the session as it will be once the terminating pods relevant to
// the task on the node are gone: it evicts them through the regular eviction path, then rolls back.
func (ssn *Session) WithoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo, fn func()) {
	if ssn.terminatingPodsMatterForAffinity(task.Pod) {
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
		// The list is built once per session; the pod's status may have changed since.
		if !isTerminating(pod) {
			continue
		}
		if err := stmt.Evict(pod, "", eviction_info.EvictionMetadata{}); err != nil {
			log.InfraLogger.V(6).Do(func() {
				log.InfraLogger.Infof("Failed to set aside terminating pod <%s/%s>: %v",
					pod.Namespace, pod.Name, err)
			})
		}
	}
	fn()
	if err := stmt.Rollback(0); err != nil {
		log.InfraLogger.Errorf("Failed to restore terminating pods: %v", err)
	}
}

// terminatingPodsFor returns the terminating pods to evict for the task on the node: the node's own
// pods, plus the DRA holders on other nodes when the task's devices may come from a pool shared
// across nodes. Inter-pod affinity looks past the node too, but its filter reads only the prefilter counts.
func (ssn *Session) terminatingPodsFor(task *pod_info.PodInfo, node *node_info.NodeInfo) []*pod_info.PodInfo {
	t := ssn.terminating()
	pods := t.byNode[node.Name]
	if !t.hasSharedDevicePool || !usesDRA(task) {
		return pods
	}
	pods = slices.Clip(pods)
	for _, holder := range t.draHolders {
		if holder.NodeName != node.Name {
			pods = append(pods, holder)
		}
	}
	return pods
}

func isTerminating(task *pod_info.PodInfo) bool {
	return task.Status == pod_status.Releasing && !task.IsVirtualStatus
}

func usesDRA(task *pod_info.PodInfo) bool {
	return len(task.Pod.Spec.ResourceClaims) > 0 || task.ExtendedResourceClaim != nil
}
