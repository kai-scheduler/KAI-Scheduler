// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package consolidation

import (
	"fmt"
	"sort"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type relocation struct {
	job   *podgroup_info.PodGroupInfo
	tasks []*pod_info.PodInfo
}

// defragment repacks running jobs onto nodes that are already in use so other nodes
// become empty. The pass is off unless DefragmentationInterval is positive.
func (alloc *consolidationAction) defragment(
	ssn *framework.Session, actionBudget *solvers.ActionSearchBudget,
) error {
	interval := ssn.SchedulerParams.DefragmentationInterval
	if interval == 0 {
		return nil
	}
	if interval < 0 {
		return fmt.Errorf("defragmentation interval must be non-negative, got %s", interval)
	}

	now := alloc.clock()
	if !alloc.lastDefragmentation.IsZero() && now.Before(alloc.lastDefragmentation.Add(interval)) {
		log.InfraLogger.V(4).Infof("Skipping defragmentation until %s", alloc.lastDefragmentation.Add(interval))
		return nil
	}
	alloc.lastDefragmentation = now

	if actionBudget != nil && actionBudget.Exhausted() {
		log.InfraLogger.V(3).Infof("Skipping defragmentation, consolidation search budget is exhausted")
		return nil
	}
	if err := validatePlacements(ssn); err != nil {
		return err
	}

	log.InfraLogger.V(3).Infof("Attempting defragmentation")
	attempted := map[string]bool{}
	progress := false
	for {
		if actionBudget != nil && actionBudget.Exhausted() {
			log.InfraLogger.V(3).Infof("Stopping defragmentation, consolidation search budget is exhausted")
			return nil
		}
		nodeName, jobs, ok := nextDefragmentationCandidate(ssn, attempted)
		if !ok {
			if !progress {
				return nil
			}
			attempted = map[string]bool{}
			progress = false
			continue
		}
		attempted[nodeName] = true
		if emptyNode(ssn, nodeName, jobs) {
			progress = true
			log.InfraLogger.V(3).Infof("Defragmentation emptied node <%s>", nodeName)
		}
	}
}

func (alloc *consolidationAction) clock() time.Time {
	if alloc.now != nil {
		return alloc.now()
	}
	return time.Now()
}

func validatePlacements(ssn *framework.Session) error {
	for _, job := range sortedJobs(ssn) {
		for _, task := range sortedTasks(job) {
			if task.NodeName == "" {
				if pod_status.AllocatedStatus(task.Status) {
					return fmt.Errorf("allocated pod %s/%s has no node", task.Namespace, task.Name)
				}
				continue
			}
			if _, found := ssn.ClusterInfo.Nodes[task.NodeName]; !found {
				return fmt.Errorf("node %q for pod %s/%s was not found", task.NodeName, task.Namespace, task.Name)
			}
			if !pod_status.AllocatedStatus(task.Status) {
				continue
			}
			if job.UID == "" {
				return fmt.Errorf("podgroup %s/%s has an empty UID", job.Namespace, job.Name)
			}
			if task.UID == "" {
				return fmt.Errorf("allocated pod %s/%s has an empty UID", task.Namespace, task.Name)
			}
		}
	}
	return nil
}

func nextDefragmentationCandidate(ssn *framework.Session, attempted map[string]bool) (string, []relocation, bool) {
	type candidate struct {
		name string
		gpus float64
		jobs []relocation
	}
	var candidates []candidate
	for name := range ssn.ClusterInfo.Nodes {
		if name == "" || attempted[name] {
			continue
		}
		gpus, jobs, ok := planNode(ssn, name)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{name: name, gpus: gpus, jobs: jobs})
	}
	if len(candidates) == 0 {
		return "", nil, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].gpus != candidates[j].gpus {
			return candidates[i].gpus < candidates[j].gpus
		}
		return candidates[i].name < candidates[j].name
	})
	chosen := candidates[0]
	return chosen.name, chosen.jobs, true
}

// planNode returns the jobs that must all move together for the node to become empty.
// A node is skipped when any pod still in use cannot be moved with a known outcome.
func planNode(ssn *framework.Session, nodeName string) (float64, []relocation, bool) {
	if nodeName == "" {
		return 0, nil, false
	}
	seen := map[common_info.PodGroupID]relocation{}
	var gpus float64
	occupied := false
	for _, job := range sortedJobs(ssn) {
		for _, task := range sortedTasks(job) {
			if task.NodeName != nodeName || !pod_status.IsActiveUsedStatus(task.Status) {
				continue
			}
			occupied = true
			node, tasks, ok := relocationOf(job)
			if !ok || node != nodeName || job.UID == "" {
				return 0, nil, false
			}
			if _, already := seen[job.UID]; !already {
				seen[job.UID] = relocation{job: job, tasks: tasks}
			}
			if pod_status.AllocatedStatus(task.Status) {
				gpus += task.GpuRequirement.GPUs()
			}
		}
	}
	if !occupied || len(seen) == 0 {
		return 0, nil, false
	}
	jobs := make([]relocation, 0, len(seen))
	ids := make([]common_info.PodGroupID, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return string(ids[i]) < string(ids[j])
	})
	for _, id := range ids {
		jobs = append(jobs, seen[id])
	}
	return gpus, jobs, true
}

// relocationOf reports the single node a fully running job can leave.
// Jobs that are pending, releasing, pipelined, or spread across nodes are not moved.
func relocationOf(job *podgroup_info.PodGroupInfo) (string, []*pod_info.PodInfo, bool) {
	if job == nil || job.UID == "" {
		return "", nil, false
	}
	var nodeName string
	var tasks []*pod_info.PodInfo
	for _, task := range sortedTasks(job) {
		if !pod_status.IsActiveUsedStatus(task.Status) &&
			task.Status != pod_status.Pending &&
			task.Status != pod_status.Gated &&
			task.Status != pod_status.Unknown {
			continue
		}
		if !pod_status.AllocatedStatus(task.Status) || task.UID == "" || task.NodeName == "" {
			return "", nil, false
		}
		if nodeName == "" {
			nodeName = task.NodeName
		} else if nodeName != task.NodeName {
			return "", nil, false
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 || nodeName == "" {
		return "", nil, false
	}
	return nodeName, tasks, true
}

func emptyNode(ssn *framework.Session, nodeName string, jobs []relocation) bool {
	destinations := destinationNodes(ssn, nodeName)
	if len(destinations) == 0 {
		return false
	}
	stmt := ssn.Statement()
	var tasks []*pod_info.PodInfo
	for _, reloc := range jobs {
		tasks = append(tasks, reloc.tasks...)
	}
	for _, task := range tasks {
		err := stmt.Evict(task, api.GetConsolidateMessage(task), eviction_info.EvictionMetadata{
			Action:           string(framework.Consolidation),
			EvictionGangSize: len(tasks),
		})
		if err != nil {
			log.InfraLogger.Errorf("Failed to evict <%s/%s> for defragmentation: %v", task.Namespace, task.Name, err)
			stmt.Discard()
			return false
		}
	}
	for _, reloc := range jobs {
		if !reallocateJob(ssn, stmt, destinations, reloc.job) {
			stmt.Discard()
			return false
		}
	}
	for _, task := range tasks {
		if task.Status != pod_status.Pipelined || task.NodeName == "" || task.NodeName == nodeName {
			stmt.Discard()
			return false
		}
	}
	if nodeHasActivePod(ssn, nodeName) {
		stmt.Discard()
		return false
	}
	if err := stmt.Commit(); err != nil {
		log.InfraLogger.Errorf("Failed to commit defragmentation statement: %v", err)
		return false
	}
	return true
}

func reallocateJob(
	ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo, job *podgroup_info.PodGroupInfo,
) bool {
	for {
		pending := podgroup_info.GetTasksToAllocate(
			job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, podgroup_info.VictimReallocation)
		if len(pending) == 0 {
			return true
		}
		if !common.AllocateJob(ssn, stmt, nodes, job, podgroup_info.VictimReallocation) {
			return false
		}
		remaining := podgroup_info.GetTasksToAllocate(
			job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, podgroup_info.VictimReallocation)
		if len(remaining) >= len(pending) {
			return false
		}
	}
}

// destinationNodes are nodes that already host a running or pipelined pod.
// Empty nodes are excluded so a move creates an empty node instead of swapping two occupied ones.
func destinationNodes(ssn *framework.Session, source string) []*node_info.NodeInfo {
	var names []string
	for name := range ssn.ClusterInfo.Nodes {
		if name == "" || name == source || !nodeHasPlacement(ssn, name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	nodes := make([]*node_info.NodeInfo, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, ssn.ClusterInfo.Nodes[name])
	}
	return nodes
}

func nodeHasPlacement(ssn *framework.Session, nodeName string) bool {
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range job.GetAllPodsMap() {
			if task.NodeName == nodeName &&
				(pod_status.AllocatedStatus(task.Status) || task.Status == pod_status.Pipelined) {
				return true
			}
		}
	}
	return false
}

func nodeHasActivePod(ssn *framework.Session, nodeName string) bool {
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for _, task := range job.GetAllPodsMap() {
			if task.NodeName == nodeName && pod_status.IsActiveUsedStatus(task.Status) {
				return true
			}
		}
	}
	return false
}

func sortedJobs(ssn *framework.Session) []*podgroup_info.PodGroupInfo {
	jobs := make([]*podgroup_info.PodGroupInfo, 0, len(ssn.ClusterInfo.PodGroupInfos))
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Namespace != jobs[j].Namespace {
			return jobs[i].Namespace < jobs[j].Namespace
		}
		if jobs[i].Name != jobs[j].Name {
			return jobs[i].Name < jobs[j].Name
		}
		return string(jobs[i].UID) < string(jobs[j].UID)
	})
	return jobs
}

func sortedTasks(job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	if job == nil {
		return nil
	}
	tasks := make([]*pod_info.PodInfo, 0, len(job.GetAllPodsMap()))
	for _, task := range job.GetAllPodsMap() {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Namespace != tasks[j].Namespace {
			return tasks[i].Namespace < tasks[j].Namespace
		}
		if tasks[i].Name != tasks[j].Name {
			return tasks[i].Name < tasks[j].Name
		}
		return string(tasks[i].UID) < string(tasks[j].UID)
	})
	return tasks
}
