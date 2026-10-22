// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/gpu_sharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

func AllocateJob(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, allocationMode podgroup_info.TaskAllocationMode) bool {
	ssn.PreJobAllocation(job)

	isPipelineOnly := allocationMode != podgroup_info.RealTaskAllocation

	// For preempt/reclaim victim reallocation, prefer each evicted task's
	// original node before any cross-node packing (#2426). Try every Releasing
	// task (not only GetTasksToAllocate's subset), then keep the restores only
	// if the job's min requirement is still satisfied — otherwise roll back so
	// gangs/complete-eviction scenarios stay fully Releasing.
	if allocationMode == podgroup_info.VictimReallocation {
		// On failure, victims remain Releasing so gang complete-eviction is preserved.
		_ = tryRestoreVictimsOnOriginalNodes(ssn, stmt, nodes, job, isPipelineOnly)
	}

	tasksToAllocate := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, allocationMode)
	if len(tasksToAllocate) == 0 {
		// Victim reallocation can finish with an empty set when every task that
		// still fits was already unevicted onto its original node.
		return allocationMode == podgroup_info.VictimReallocation
	}

	result := ssn.IsJobOverQueueCapacityFn(job, tasksToAllocate)
	if !result.IsSchedulable {
		if !isPipelineOnly {
			job.AddJobFitError(common_info.NewJobFitErrorWithQueueContext(
				job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
				result.Reason, result.Message, result.Details))
		}
		return false
	}
	return allocateSubGroupSet(ssn, stmt, nodes, job, job.RootSubGroupSet, tasksToAllocate, isPipelineOnly)
}

func allocateSubGroupSet(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, subGroupSet *subgroup_info.SubGroupSet, subtreeTasksToAllocate []*pod_info.PodInfo,
	isPipelineOnly bool,
) bool {
	if len(subtreeTasksToAllocate) == 0 {
		return true
	}
	relevantPodSets := subtreePodSetsContainingTasks(subGroupSet, subtreeTasksToAllocate)
	nodeSets, err := ssn.SubsetNodesFn(job, &subGroupSet.SubGroupInfo, relevantPodSets, subtreeTasksToAllocate, nodes)
	if err != nil {
		log.InfraLogger.Errorf(
			"Failed to run SubsetNodes on job <%s/%s>: %v", job.Namespace, job.Name, err)
		return false
	}

	for _, nodeSet := range nodeSets {
		cp := stmt.Checkpoint()
		if allocateMembersOnNodes(ssn, stmt, nodeSet, job, subGroupSet, subtreeTasksToAllocate, isPipelineOnly) {
			return true
		}
		if err := stmt.Rollback(cp); err != nil {
			log.InfraLogger.Errorf("Failed to rollback statement in session %v, err: %v", ssn.ID, err)
		}
	}

	return false
}

// allocateMembersOnNodes allocates the tasks that appear in subtreeTasksToAllocate by traversing the subtree rooted at subGroupSet.
// The tasks in subtreeTasksToAllocate are the required tasks to satisfy the next step of allocation - either part of the min required subgroup or extra tasks from a satisfied subgroup.
// All members that do have tasks must succeed for this function to return true.
func allocateMembersOnNodes(ssn *framework.Session, stmt *framework.Statement, nodes node_info.NodeSet,
	job *podgroup_info.PodGroupInfo, subGroupSet *subgroup_info.SubGroupSet, subtreeTasksToAllocate []*pod_info.PodInfo,
	isPipelineOnly bool,
) bool {
	for _, memberGeneric := range orderedMembers(ssn, subGroupSet.GetMembers()) {
		switch member := memberGeneric.(type) {
		case *subgroup_info.PodSet:
			if !allocatePodSet(ssn, stmt, nodes, job, member,
				filterTasksForPodSet(member, subtreeTasksToAllocate), isPipelineOnly) {
				return false
			}
		case *subgroup_info.SubGroupSet:
			if !allocateSubGroupSet(ssn, stmt, nodes, job, member,
				filterTasksForPodSets(member.GetDescendantPodSets(), subtreeTasksToAllocate), isPipelineOnly) {
				return false
			}
		}
	}
	return true
}

// subtreePodSetsContainingTasks returns only the PodSets that are a descendant of the given SubGroupSet and that have at least one task in the list.
func subtreePodSetsContainingTasks(subGroupSet *subgroup_info.SubGroupSet, tasks []*pod_info.PodInfo) map[string]*subgroup_info.PodSet {
	allPodSets := subGroupSet.GetDescendantPodSets()
	result := make(map[string]*subgroup_info.PodSet)
	for _, task := range tasks {
		name := task.SubGroupName
		if len(name) == 0 {
			name = podgroup_info.DefaultSubGroup
		}
		if ps, ok := allPodSets[name]; ok {
			result[name] = ps
		}
	}
	return result
}

func allocatePodSet(ssn *framework.Session, stmt *framework.Statement, nodes node_info.NodeSet,
	job *podgroup_info.PodGroupInfo, podSet *subgroup_info.PodSet, podsetTasksToAllocate []*pod_info.PodInfo,
	isPipelineOnly bool,
) bool {
	if len(podsetTasksToAllocate) == 0 {
		return true
	}
	podSets := map[string]*subgroup_info.PodSet{
		podSet.GetName(): podSet,
	}
	nodeSets, err := ssn.SubsetNodesFn(job, &podSet.SubGroupInfo, podSets, podsetTasksToAllocate, nodes)
	if err != nil {
		log.InfraLogger.Errorf(
			"Failed to run SubsetNodes on job <%s/%s>: %v", job.Namespace, job.Name, err)
		return false
	}

	for _, nodeSet := range nodeSets {
		cp := stmt.Checkpoint()
		if allocateTasksOnNodeSet(ssn, stmt, nodeSet, job, podsetTasksToAllocate, isPipelineOnly) {
			return true
		}
		if err := stmt.Rollback(cp); err != nil {
			log.InfraLogger.Errorf("Failed to rollback statement in session %v, err: %v", ssn.ID, err)
		}
	}
	return false
}

func allocateTasksOnNodeSet(ssn *framework.Session, stmt *framework.Statement, nodes node_info.NodeSet,
	job *podgroup_info.PodGroupInfo, tasksToAllocate []*pod_info.PodInfo, isPipelineOnly bool) bool {
	for index, task := range tasksToAllocate {
		success := allocateTask(ssn, stmt, nodes, task, isPipelineOnly)
		if !success {
			handleFailedTaskAllocation(job, task, index)
			return false
		}
	}
	return true
}

func allocateTask(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	task *pod_info.PodInfo, isPipelineOnly bool) (success bool) {
	job := ssn.ClusterInfo.PodGroupInfos[task.Job]
	if job == nil {
		log.InfraLogger.Errorf("Failed to find job <%s> in session <%s>", task.Job, ssn.ID)
		return false
	}
	err := ssn.PrePredicateFn(task, job)
	if err != nil {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("pre-predicates failed on task %s/%s. Error: %v",
				task.Namespace, task.Name, err)
		})

		fitErrors := common_info.NewFitErrors()
		fitErrors.SetError(err.Error())
		job.AddTaskFitErrors(task, fitErrors)
		return false
	}

	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("Looking for best node for task - Task: <%s/%s>, init requested: <%v>.",
			task.Namespace, task.Name, task.ResReqVector)
	})

	orderedNodes := ssn.OrderedNodesByTask(nodes, task)
	// Evicted victims keep their original NodeName while Releasing. Prefer that
	// node first so a victim that still fits at home is unevicted instead of
	// being pipelined elsewhere and then deleted on preempt/reclaim commit (#2426).
	if task.Status == pod_status.Releasing && task.NodeName != "" {
		orderedNodes = preferNodeFirst(orderedNodes, task.NodeName)
	}
	for _, node := range orderedNodes {
		if !ssn.FittingNode(task, node, !isPipelineOnly) {
			continue
		}
		success = allocateTaskToNode(ssn, stmt, task, node, isPipelineOnly)
		if success {
			break
		}

		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("Failed to allocate or pipeline task: <%v/%v> to node: %v",
				task.Namespace, task.Name, node.Name)
		})
	}

	if success {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("Allocation succeeded for task: <%v/%v>", task.Namespace, task.Name)
		})
	} else {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("Failed statement allocate for task: <%v/%v>", task.Namespace, task.Name)
		})
	}

	return success
}

func allocateTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, isPipelineOnly bool) bool {
	placement, err := ssn.GetNumaPlacement(task, node)
	if err != nil {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("Failed to solve NUMA placement for task <%s/%s> on node %s: %v", task.Namespace, task.Name, node.Name, err)
		})
		return false
	}
	task.NUMAPlacement = placement

	if task.IsFractionRequest() || task.IsGpuMemoryRequest() {
		return gpu_sharing.AllocateFractionalGPUTaskToNode(ssn, stmt, task, node, isPipelineOnly)
	}

	if taskAllocatable := node.IsTaskAllocatable(task); !isPipelineOnly && taskAllocatable {
		return bindTaskToNode(ssn, stmt, task, node)
	}
	return pipelineTaskToNode(ssn, stmt, task, node, !isPipelineOnly)
}

func bindTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("Binding Task <%v/%v> to node <%v>, requires resources: %v",
			task.Namespace, task.Name, node.Name, task.ResReqVector)
	})

	if err := stmt.Allocate(task, node.Name); err != nil {
		log.InfraLogger.Errorf("Failed to bind Task %v on %v in Session %v, err: %v", task.UID, node.Name, ssn.ID, err)
		return false
	}
	return true
}

func pipelineTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, updateTasksIfExistsOnNode bool) bool {
	log.InfraLogger.V(6).Do(func() {
		log.InfraLogger.Infof("Pipelining Task <%v/%v> to node <%v>, requires resources: %v",
			task.Namespace, task.Name, node.Name, task.ResReqVector)
	})

	if err := stmt.Pipeline(task, node.Name, updateTasksIfExistsOnNode); err != nil {
		log.InfraLogger.V(6).Do(func() {
			log.InfraLogger.Infof("Failed to pipeline Task %v on %v in Session %v", task.UID, node.Name, ssn.ID)
		})
		return false
	}
	return true
}

func handleFailedTaskAllocation(job *podgroup_info.PodGroupInfo, unschedulableTask *pod_info.PodInfo, numSchedulableTasks int) {
	allocationError, found := job.TasksFitErrors[unschedulableTask.UID]

	if !found {
		allocationError = common_info.NewFitErrors()
		allocationError.SetError(common_info.DefaultPodError)
	}

	gangScheduling := isGangScheduling(job)
	taskSubGroupName := podgroup_info.DefaultSubGroup
	if len(unschedulableTask.SubGroupName) != 0 {
		taskSubGroupName = unschedulableTask.SubGroupName
	}
	taskSubGroup := job.GetAllPodSets()[taskSubGroupName]

	if !gangScheduling || taskSubGroup.GetNumActiveUsedTasks() >= int(taskSubGroup.GetMinAvailable()) {
		job.AddSimpleJobFitError(
			podgroup_info.PodSchedulingErrors,
			fmt.Sprintf("Resources were not found for pod %s/%s due to: %s",
				unschedulableTask.Namespace, unschedulableTask.Name, allocationError.Error()))
		return
	}

	if len(job.GetAllPodSets()) == 1 && taskSubGroup.GetName() == podgroup_info.DefaultSubGroup {
		job.AddSimpleJobFitError(
			podgroup_info.PodSchedulingErrors,
			fmt.Sprintf("Resources were found for %d pods while %d are required for gang scheduling. "+
				"Additional pods cannot be scheduled due to: %s",
				numSchedulableTasks, taskSubGroup.GetMinAvailable(), allocationError.Error()))
		return
	}
	job.AddSimpleJobFitError(
		podgroup_info.PodSchedulingErrors,
		fmt.Sprintf("Resources were found for %d pods from all sub-groups while sub-group %s requires %d pods for gang scheduling. "+
			"Additional pods cannot be scheduled in this sub-group due to: %s",
			numSchedulableTasks, taskSubGroup.GetName(), taskSubGroup.GetMinAvailable(), allocationError.Error()))
}

func isGangScheduling(job *podgroup_info.PodGroupInfo) bool {
	for _, subGroup := range job.GetAllPodSets() {
		if subGroup.GetMinAvailable() > 1 {
			return true
		}
	}
	return false
}

func filterTasksForPodSet(podSet *subgroup_info.PodSet, tasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	return filterTasksForPodSets(map[string]*subgroup_info.PodSet{podSet.GetName(): podSet}, tasks)
}

func filterTasksForPodSets(podSets map[string]*subgroup_info.PodSet, tasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	var result []*pod_info.PodInfo
	for _, task := range tasks {
		subGroupName := task.SubGroupName
		if len(subGroupName) == 0 {
			subGroupName = podgroup_info.DefaultSubGroup
		}
		if _, found := podSets[subGroupName]; found {
			result = append(result, task)
		}
	}
	return result
}

func orderedMembers(ssn *framework.Session, subGroupMembers []subgroup_info.SubGroupMember) []subgroup_info.SubGroupMember {
	sort.SliceStable(subGroupMembers, func(i, j int) bool {
		return ssn.SubGroupOrderFn(subGroupMembers[i], subGroupMembers[j])
	})
	return subGroupMembers
}

// preferNodeFirst moves the named node to the front of the list when present.
// Other nodes keep their relative order.
func preferNodeFirst(nodes []*node_info.NodeInfo, nodeName string) []*node_info.NodeInfo {
	if nodeName == "" || len(nodes) == 0 {
		return nodes
	}
	preferred := make([]*node_info.NodeInfo, 0, 1)
	rest := make([]*node_info.NodeInfo, 0, len(nodes))
	for _, node := range nodes {
		if node != nil && node.Name == nodeName {
			preferred = append(preferred, node)
			continue
		}
		rest = append(rest, node)
	}
	if len(preferred) == 0 {
		return nodes
	}
	return append(preferred, rest...)
}

// tryRestoreVictimsOnOriginalNodes tries every Releasing task of the job only
// on its original node. If the job's min requirement is satisfied afterwards,
// the unevicts are kept so later packing cannot steal those home nodes.
// Otherwise the attempt is rolled back and the caller continues with the
// previous Releasing state (complete gang eviction).
func tryRestoreVictimsOnOriginalNodes(
	ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, isPipelineOnly bool,
) bool {
	tasks := releasingTasksOfJob(job)
	if len(tasks) == 0 {
		return podgroup_info.IsMinRequirementSatisfied(job)
	}

	cp := stmt.Checkpoint()
	for _, task := range tasks {
		original := nodeByName(nodes, task.NodeName)
		if original == nil {
			continue
		}
		_ = allocateTask(ssn, stmt, []*node_info.NodeInfo{original}, task, isPipelineOnly)
	}

	if podgroup_info.IsMinRequirementSatisfied(job) {
		return true
	}
	if err := stmt.Rollback(cp); err != nil {
		log.InfraLogger.Errorf("Failed to rollback original-node victim restore in session %v, err: %v",
			ssn.ID, err)
	}
	return false
}

func releasingTasksOfJob(job *podgroup_info.PodGroupInfo) []*pod_info.PodInfo {
	var tasks []*pod_info.PodInfo
	for _, task := range job.GetAllPodsMap() {
		if task.Status == pod_status.Releasing && task.NodeName != "" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func nodeByName(nodes []*node_info.NodeInfo, name string) *node_info.NodeInfo {
	for _, node := range nodes {
		if node != nil && node.Name == name {
			return node
		}
	}
	return nil
}
