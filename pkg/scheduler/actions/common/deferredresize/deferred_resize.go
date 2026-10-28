// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package deferredresize lets preempt and reclaim free room for kubelet-Deferred in-place resizes, as
// KEP-5836 does in kube-scheduler. The resizing pod is never evicted or re-placed; only victims are.
package deferredresize

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

type Solve func(job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) (*framework.Statement, *solvers.SearchResult)

func NonPendingJobFilter(ssn *framework.Session) func(*podgroup_info.PodGroupInfo) bool {
	if !ssn.DeferredResizePreemptionEnabled() {
		return nil
	}
	return func(job *podgroup_info.PodGroupInfo) bool {
		for _, task := range job.GetAllPodsMap() {
			if pod_info.IsDeferredResizeTask(task) {
				return true
			}
		}
		return false
	}
}

func HasActiveAllocatedTaskOnNode(job *podgroup_info.PodGroupInfo, nodeName string) bool {
	for _, task := range job.GetAllPodsMap() {
		if task.NodeName == nodeName && pod_status.IsActiveAllocatedStatus(task.Status) {
			return true
		}
	}
	return false
}

func AttemptForJob(
	ssn *framework.Session, job *podgroup_info.PodGroupInfo,
	actionType framework.ActionType, solve Solve, shouldStopForResult func(*solvers.SearchResult) bool,
) (shouldStop bool) {
	// A job below its gang minimum must not evict others so one of its pods can grow.
	if !ssn.DeferredResizePreemptionEnabled() || !job.RootSubGroupSet.IsMinRequirementSatisfied() {
		return false
	}
	for _, task := range deferredTasks(ssn, job) {
		node := ssn.ClusterInfo.Nodes[task.NodeName]
		// Also true while earlier victims terminate, so no more are evicted.
		if node.IsInPlaceResizeAllocatable(task) {
			continue
		}
		metrics.IncPodgroupsConsideredByAction()
		statement, result := solve(resizeRepresentative(job, task), node)
		if statement == nil {
			if shouldStopForResult(result) {
				return true
			}
			log.InfraLogger.V(3).Infof("Didn't find a %s strategy for deferred resize of <%s/%s> on node <%s>",
				actionType, task.Namespace, task.Name, node.Name)
			continue
		}
		if err := statement.Commit(); err != nil {
			log.InfraLogger.Errorf("Failed to commit %s statement for deferred resize: %v", actionType, err)
			continue
		}
		metrics.IncPodgroupScheduledByAction()
		log.InfraLogger.V(3).Infof("Freed room for deferred resize of <%s/%s> on node <%s>",
			task.Namespace, task.Name, node.Name)
	}
	return false
}

func deferredTasks(ssn *framework.Session, job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, task := range job.GetAllPodsMap() {
		if _, found := ssn.ClusterInfo.Nodes[task.NodeName]; found && pod_info.IsDeferredResizeTask(task) {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return ssn.TaskOrderFn(tasks[i], tasks[j]) })
	return tasks
}

// resizeRepresentative swaps the resizing pod for its stand-in and leaves out pending pods, which
// were handled first.
func resizeRepresentative(job *podgroup_info.PodGroupInfo, task *pod_info.PodInfo) *podgroup_info.PodGroupInfo {
	tasks := []*pod_info.PodInfo{pod_info.NewResizeStandIn(task)}
	for _, allocated := range job.GetAllAllocatedPods() {
		if allocated.UID != task.UID {
			tasks = append(tasks, allocated)
		}
	}
	return job.CloneWithTasks(tasks)
}
