// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// terminatingPods are the pods that were already terminating when the session opened: Releasing,
// but not evicted by this session. Their resources already count as releasing, yet they still hold
// their host ports, pod (anti-)affinity and DRA devices.
type terminatingPods struct {
	byNode                  map[string][]*pod_info.PodInfo
	all                     []*pod_info.PodInfo
	hasRequiredAntiAffinity bool
	// hasSharedDevicePool is set when a DRA device pool is reachable from more than one node.
	hasSharedDevicePool bool
}

func (ssn *Session) terminating() *terminatingPods {
	if ssn.terminatingPods != nil {
		return ssn.terminatingPods
	}
	t := &terminatingPods{byNode: map[string][]*pod_info.PodInfo{}}
	for _, node := range ssn.ClusterInfo.Nodes {
		for _, task := range node.PodInfos {
			if !isTerminating(task) {
				continue
			}
			// The node holds a copy; eviction must act on the job's task.
			job := ssn.ClusterInfo.PodGroupInfos[task.Job]
			if job == nil {
				continue
			}
			jobTask := job.GetAllPodsMap()[task.UID]
			if jobTask == nil {
				continue
			}
			t.byNode[node.Name] = append(t.byNode[node.Name], jobTask)
			t.all = append(t.all, jobTask)
			if len(requiredAntiAffinityTerms(task.Pod)) > 0 {
				t.hasRequiredAntiAffinity = true
			}
		}
	}
	for _, slice := range ssn.ClusterInfo.ResourceSlices {
		if slice.Spec.NodeName == nil || *slice.Spec.NodeName == "" {
			t.hasSharedDevicePool = true
		}
	}
	ssn.terminatingPods = t
	return t
}

func isTerminating(task *pod_info.PodInfo) bool {
	return task.Status == pod_status.Releasing && !task.IsVirtualStatus
}

// terminatingPodsFor returns the terminating pods whose removal can change the task's predicates on
// the node: the node's own pods, or every terminating pod when pod (anti-)affinity or a shared DRA
// device pool is involved, since those look past the node.
func (ssn *Session) terminatingPodsFor(task *pod_info.PodInfo, node *node_info.NodeInfo) []*pod_info.PodInfo {
	t := ssn.terminating()
	if len(t.all) == 0 {
		return nil
	}
	if t.hasRequiredAntiAffinity || hasPodAffinityTerms(task.Pod) || (t.hasSharedDevicePool && UsesDRA(task)) {
		return t.all
	}
	return t.byNode[node.Name]
}

// WithoutTerminatingPods runs fn while the terminating pods relevant to the task are evicted from the
// session, then rolls the evictions back. fn sees the cluster as it will be once those pods are gone.
func (ssn *Session) WithoutTerminatingPods(task *pod_info.PodInfo, node *node_info.NodeInfo, fn func() bool) bool {
	pods := ssn.terminatingPodsFor(task, node)
	if len(pods) == 0 {
		return fn()
	}
	// A scratch statement keeps the caller's statement and its checkpoints untouched.
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
	result := fn()
	if err := stmt.Rollback(0); err != nil {
		log.InfraLogger.Errorf("Failed to restore terminating pods after a simulation: %v", err)
	}
	return result
}

// fitsWithoutTerminatingPods reports whether the task passes its predicates on the node once the
// terminating pods are gone. Queue capacity is still checked against the current cluster, since
// terminating pods count against their queue until they leave.
func (ssn *Session) fitsWithoutTerminatingPods(
	task *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
) error {
	if len(ssn.terminatingPodsFor(task, node)) == 0 {
		return ssn.PredicateFn(task, job, node)
	}
	var err error
	ssn.WithoutTerminatingPods(task, node, func() bool {
		// Pod affinity keeps per-pod prefilter state; recompute it without the terminating pods.
		if err = ssn.PrePredicateFn(task, job); err == nil {
			err = ssn.PredicateFn(task, job, node)
		}
		return err == nil
	})
	// Restore the prefilter state of the current cluster.
	_ = ssn.PrePredicateFn(task, job)
	if err != nil {
		return err
	}
	if result := ssn.IsTaskAllocationOnNodeOverCapacityFn(task, job, node); !result.IsSchedulable {
		return common_info.NewFitError(task.Name, task.Namespace, node.Name,
			fmt.Sprintf("node does not have enough capacity. Reason: %s, Details: %s", result.Reason, result.Message))
	}
	return nil
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
