// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package deferredresize lets preempt and reclaim free room on a node for in-place resizes
// that the kubelet marked as Deferred.
//
// The resizing pods are virtually evicted inside a statement so the job solver sees them as
// pending at their desired size, pinned to their own node. On success only the victim
// evictions are committed; the resizing pods are never evicted.
package deferredresize

import (
	"sort"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/metrics"
)

// Solve runs the action's admission check and job solver for one virtually evicted resizing task.
// The solver must be pinned to node (an in-place resize cannot move the pod) and take victims from
// node only. A nil statement means no solution.
type Solve func(
	job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo, task *pod_info.PodInfo,
) (*framework.Statement, *solvers.SearchResult)

// hasDeferredResize reports whether any pod of the job has a deferred in-place upsize.
func hasDeferredResize(job *podgroup_info.PodGroupInfo) bool {
	for _, task := range job.GetAllPodsMap() {
		if pod_info.IsDeferredResizeTask(task) {
			return true
		}
	}
	return false
}

// NonPendingJobFilter keeps jobs without pending pods in an action's job queue when they have
// a deferred resize. It is nil when the feature is disabled.
func NonPendingJobFilter(ssn *framework.Session) func(*podgroup_info.PodGroupInfo) bool {
	if !ssn.DeferredResizePreemptionEnabled() {
		return nil
	}
	return hasDeferredResize
}

// HasActiveAllocatedTaskOnNode reports whether a candidate victim job can free room on the node.
func HasActiveAllocatedTaskOnNode(job *podgroup_info.PodGroupInfo, nodeName string) bool {
	for _, task := range job.GetAllPodsMap() {
		if task.NodeName == nodeName && pod_status.IsActiveAllocatedStatus(task.Status) {
			return true
		}
	}
	return false
}

// AttemptForJob tries each deferred task of the job, found by scanning the job when it is popped
// so earlier evictions and moves in the session are taken into account. It returns true when the
// action should stop, as judged by shouldStopForResult on a failed attempt (e.g. the search
// budget is exhausted).
func AttemptForJob(
	ssn *framework.Session, job *podgroup_info.PodGroupInfo,
	actionType framework.ActionType, solve Solve, shouldStopForResult func(*solvers.SearchResult) bool,
) (shouldStop bool) {
	if !ssn.DeferredResizePreemptionEnabled() || !isGangSecured(job) {
		return false
	}
	for _, task := range deferredTasks(ssn, job) {
		metrics.IncPodgroupsConsideredByAction()
		succeeded, result := attempt(ssn, job, ssn.ClusterInfo.Nodes[task.NodeName], task, actionType, solve)
		if succeeded {
			continue
		}
		if shouldStopForResult(result) {
			return true
		}
		log.InfraLogger.V(3).Infof("Didn't find a %s strategy for deferred resize of <%s/%s> on node <%s>",
			actionType, task.Namespace, task.Name, task.NodeName)
	}
	return false
}

// deferredTasks returns the job's deferred resize tasks on known nodes, sorted by name.
func deferredTasks(ssn *framework.Session, job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, task := range job.GetAllPodsMap() {
		if _, found := ssn.ClusterInfo.Nodes[task.NodeName]; found && pod_info.IsDeferredResizeTask(task) {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	return tasks
}

// attempt frees room on node for one deferred resize task, which must be current (as found by the
// scan in AttemptForJob). One task per attempt is enough: other deferred tasks on the node are
// already charged at their desired size, so this task fits back only once the whole node
// shortfall is covered. It also matters for correctness: with several tasks, the solver re-picks
// them by the job's gang rule and may place only some, but one task is the only one it can pick.
// The job's own attempts never evict its own pods and discard their statements, so the task stays
// current across the job's attempts. succeeded is true without evictions when the node already has room.
func attempt(
	ssn *framework.Session, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo,
	task *pod_info.PodInfo, actionType framework.ActionType, solve Solve,
) (succeeded bool, result *solvers.SearchResult) {
	if nodeFitsAfterReleasing(node) {
		return true, nil
	}

	self := ssn.Statement()
	defer self.Discard()
	if err := self.Evict(task, "", eviction_info.EvictionMetadata{}); err != nil {
		log.InfraLogger.Errorf("Failed to virtually evict deferred resize task <%s/%s>: %v",
			task.Namespace, task.Name, err)
		return false, nil
	}

	stmt, result := solve(job, node, task)
	if stmt == nil {
		return false, result
	}
	defer stmt.Discard()
	// JobSolver reports unsolved here since a Releasing task already counts as active.
	victims := stmt.EvictedTasks()
	if result.Reason() != solvers.SearchResultSolved || len(victims) == 0 {
		return false, result
	}
	// Commit the victims' evictions and moves as solved, without the resizing pod's placement.
	if err := stmt.Unpipeline(task); err != nil {
		log.InfraLogger.Errorf("Failed to undo the placement of deferred resize task <%s/%s>: %v",
			task.Namespace, task.Name, err)
		return false, result
	}
	if err := stmt.Commit(); err != nil {
		log.InfraLogger.Errorf("Failed to commit %s statement for deferred resize: %v", actionType, err)
	}
	metrics.IncPodgroupScheduledByAction()
	log.InfraLogger.V(3).Infof("Evicted <%v> for deferred resize of <%s/%s> on node <%s>",
		taskNames(victims), task.Namespace, task.Name, node.Name)
	return true, result
}

// isGangSecured reports whether the job meets its gang minimum with allocated pods. A job that
// is not yet working must not evict others so that one of its pods can grow.
func isGangSecured(job *podgroup_info.PodGroupInfo) bool {
	return job.RootSubGroupSet != nil && job.RootSubGroupSet.IsMinRequirementSatisfied()
}

// ResizeRepresentative returns a copy of the job with only its allocated pods and the given
// resizing tasks, so job-level checks do not count the demand of its pending pods.
func ResizeRepresentative(job *podgroup_info.PodGroupInfo, tasks []*pod_info.PodInfo) *podgroup_info.PodGroupInfo {
	return job.CloneWithTasks(append(job.GetAllAllocatedPods(), tasks...))
}

func nodeFitsAfterReleasing(node *node_info.NodeInfo) bool {
	for _, resource := range []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory} {
		if node.NonAllocatedResource(resource) < 0 {
			return false
		}
	}
	return true
}

func taskNames(tasks []*pod_info.PodInfo) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Namespace+"/"+task.Name)
	}
	return names
}
