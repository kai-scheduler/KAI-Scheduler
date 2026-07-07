// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/gpu_sharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// AllocationResult contains the outcome of one job allocation attempt.
type AllocationResult struct {
	Success     bool
	diagnostics *allocationDiagnostics
}

// PublishFitErrors replaces diagnostics owned by the authoritative allocation attempt.
func (r AllocationResult) PublishFitErrors(job *podgroup_info.PodGroupInfo) {
	if r.diagnostics == nil {
		return
	}
	job.ReplaceAllocationFitErrors(
		r.diagnostics.failedTask,
		r.diagnostics.taskFitError,
		r.diagnostics.jobFitErrors,
	)
}

type allocationDiagnostics struct {
	failedTask   *pod_info.PodInfo
	taskFitError *common_info.TasksFitErrors
	jobFitErrors []common_info.JobFitError
}

type allocationOutcome struct {
	success        bool
	allocatedTasks int
	diagnostics    *allocationDiagnostics
}

func selectOutcomeByAllocatedTasks(current, candidate allocationOutcome) allocationOutcome {
	if candidate.allocatedTasks > current.allocatedTasks {
		return candidate
	}
	return current
}

func AllocateJob(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, isPipelineOnly bool) AllocationResult {
	ssn.PreJobAllocation(job)
	collectFitErrors := !isPipelineOnly

	tasksToAllocate := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, !isPipelineOnly)
	if len(tasksToAllocate) == 0 {
		return newAllocationResult(false, nil, collectFitErrors)
	}

	result := ssn.IsJobOverQueueCapacityFn(job, tasksToAllocate)
	if !result.IsSchedulable {
		var diagnostics *allocationDiagnostics
		if collectFitErrors {
			diagnostics = &allocationDiagnostics{jobFitErrors: []common_info.JobFitError{
				common_info.NewJobFitErrorWithQueueContext(
					job.Name, podgroup_info.DefaultSubGroup, job.Namespace,
					result.Reason, result.Message, result.Details),
			}}
		}
		return newAllocationResult(false, diagnostics, collectFitErrors)
	}
	outcome := allocateSubGroupSet(ssn, stmt, nodes, job, job.RootSubGroupSet, tasksToAllocate, isPipelineOnly)
	return newAllocationResult(outcome.success, outcome.diagnostics, collectFitErrors)
}

func newAllocationResult(success bool, diagnostics *allocationDiagnostics, collectFitErrors bool) AllocationResult {
	if collectFitErrors && diagnostics == nil {
		diagnostics = &allocationDiagnostics{}
	}
	return AllocationResult{Success: success, diagnostics: diagnostics}
}

func allocateSubGroupSet(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, subGroupSet *subgroup_info.SubGroupSet, subtreeTasksToAllocate []*pod_info.PodInfo,
	isPipelineOnly bool,
) allocationOutcome {
	if len(subtreeTasksToAllocate) == 0 {
		return allocationOutcome{success: true}
	}
	relevantPodSets := subtreePodSetsContainingTasks(subGroupSet, subtreeTasksToAllocate)
	subsetResult, err := ssn.SubsetNodesFn(
		job, &subGroupSet.SubGroupInfo, relevantPodSets, subtreeTasksToAllocate, nodes, !isPipelineOnly,
	)
	if err != nil {
		log.InfraLogger.Errorf(
			"Failed to run SubsetNodes on job <%s/%s>: %v", job.Namespace, job.Name, err)
		return allocationOutcome{}
	}
	nodeSets := subsetResult.NodeSets
	if len(nodeSets) == 0 && len(subsetResult.FitErrors) != 0 {
		return allocationOutcome{diagnostics: &allocationDiagnostics{jobFitErrors: subsetResult.FitErrors}}
	}

	var bestFailure allocationOutcome
	hasFailure := false
	for _, nodeSet := range nodeSets {
		cp := stmt.Checkpoint()
		outcome := allocateMembersOnNodes(ssn, stmt, nodeSet, job, subGroupSet, subtreeTasksToAllocate, isPipelineOnly)
		if outcome.success {
			return outcome
		}
		if err := stmt.Rollback(cp); err != nil {
			log.InfraLogger.Errorf("Failed to rollback statement in session %v, err: %v", ssn.ID, err)
		}
		if !hasFailure {
			bestFailure = outcome
			hasFailure = true
		} else {
			bestFailure = selectOutcomeByAllocatedTasks(bestFailure, outcome)
		}
	}

	return bestFailure
}

// allocateMembersOnNodes allocates the tasks that appear in subtreeTasksToAllocate by traversing the subtree rooted at subGroupSet.
// The tasks in subtreeTasksToAllocate are the required tasks to satisfy the next step of allocation - either part of the min required subgroup or extra tasks from a satisfied subgroup.
// All members that do have tasks must succeed for this function to return true.
func allocateMembersOnNodes(ssn *framework.Session, stmt *framework.Statement, nodes node_info.NodeSet,
	job *podgroup_info.PodGroupInfo, subGroupSet *subgroup_info.SubGroupSet, subtreeTasksToAllocate []*pod_info.PodInfo,
	isPipelineOnly bool,
) allocationOutcome {
	allocatedTasks := 0
	for _, memberGeneric := range orderedMembers(ssn, subGroupSet.GetMembers()) {
		var outcome allocationOutcome
		switch member := memberGeneric.(type) {
		case *subgroup_info.PodSet:
			outcome = allocatePodSet(ssn, stmt, nodes, job, member,
				filterTasksForPodSet(member, subtreeTasksToAllocate), isPipelineOnly)
		case *subgroup_info.SubGroupSet:
			outcome = allocateSubGroupSet(ssn, stmt, nodes, job, member,
				filterTasksForPodSets(member.GetDescendantPodSets(), subtreeTasksToAllocate), isPipelineOnly)
		}
		if !outcome.success {
			outcome.allocatedTasks += allocatedTasks
			return outcome
		}
		allocatedTasks += outcome.allocatedTasks
	}
	return allocationOutcome{success: true, allocatedTasks: allocatedTasks}
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
) allocationOutcome {
	if len(podsetTasksToAllocate) == 0 {
		return allocationOutcome{success: true}
	}
	podSets := map[string]*subgroup_info.PodSet{
		podSet.GetName(): podSet,
	}
	subsetResult, err := ssn.SubsetNodesFn(
		job, &podSet.SubGroupInfo, podSets, podsetTasksToAllocate, nodes, !isPipelineOnly,
	)
	if err != nil {
		log.InfraLogger.Errorf(
			"Failed to run SubsetNodes on job <%s/%s>: %v", job.Namespace, job.Name, err)
		return allocationOutcome{}
	}
	nodeSets := subsetResult.NodeSets
	if len(nodeSets) == 0 && len(subsetResult.FitErrors) != 0 {
		return allocationOutcome{diagnostics: &allocationDiagnostics{jobFitErrors: subsetResult.FitErrors}}
	}

	var bestFailure allocationOutcome
	hasFailure := false
	for _, nodeSet := range nodeSets {
		cp := stmt.Checkpoint()
		outcome := allocateTasksOnNodeSet(ssn, stmt, nodeSet, job, podsetTasksToAllocate, isPipelineOnly)
		if outcome.success {
			return outcome
		}
		if err := stmt.Rollback(cp); err != nil {
			log.InfraLogger.Errorf("Failed to rollback statement in session %v, err: %v", ssn.ID, err)
		}
		if !hasFailure {
			bestFailure = outcome
			hasFailure = true
		} else {
			bestFailure = selectOutcomeByAllocatedTasks(bestFailure, outcome)
		}
	}
	return bestFailure
}

func allocateTasksOnNodeSet(ssn *framework.Session, stmt *framework.Statement, nodes node_info.NodeSet,
	job *podgroup_info.PodGroupInfo, tasksToAllocate []*pod_info.PodInfo, isPipelineOnly bool) allocationOutcome {
	for index, task := range tasksToAllocate {
		success, taskFitError := allocateTask(ssn, stmt, nodes, task, isPipelineOnly)
		if !success {
			var diagnostics *allocationDiagnostics
			if !isPipelineOnly {
				diagnostics = &allocationDiagnostics{
					failedTask:   task,
					taskFitError: taskFitError,
					jobFitErrors: []common_info.JobFitError{
						newFailedTaskAllocationError(job, task, index, taskFitError),
					},
				}
			}
			return allocationOutcome{allocatedTasks: index, diagnostics: diagnostics}
		}
	}
	return allocationOutcome{success: true, allocatedTasks: len(tasksToAllocate)}
}

func allocateTask(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	task *pod_info.PodInfo, isPipelineOnly bool) (success bool, taskFitError *common_info.TasksFitErrors) {
	job := ssn.ClusterInfo.PodGroupInfos[task.Job]
	if job == nil {
		log.InfraLogger.Errorf("Failed to find job <%s> in session <%s>", task.Job, ssn.ID)
		return false, nil
	}
	err := ssn.PrePredicateFn(task, job)
	if err != nil {
		log.InfraLogger.V(6).Infof("pre-predicates failed on task %s/%s. Error: %v",
			task.Namespace, task.Name, err)

		if !isPipelineOnly {
			taskFitError = common_info.NewFitErrors()
			taskFitError.SetError(err.Error())
		}
		return false, taskFitError
	}

	log.InfraLogger.V(6).Infof("Looking for best node for task - Task: <%s/%s>, init requested: <%v>.",
		task.Namespace, task.Name, task.ResReqVector)

	orderedNodes := ssn.OrderedNodesByTask(nodes, task)
	var fitErrors *common_info.TasksFitErrors
	if !isPipelineOnly {
		fitErrors = common_info.NewFitErrors()
	}
	for _, node := range orderedNodes {
		fits, fitError := ssn.FittingNode(task, node, !isPipelineOnly)
		if fitErrors != nil && fitError != nil {
			fitErrors.AddNodeError(fitError)
		}
		if !fits {
			continue
		}
		success = allocateTaskToNode(ssn, stmt, task, node, isPipelineOnly)
		if success {
			break
		}

		log.InfraLogger.V(6).Infof("Failed to allocate or pipeline task: <%v/%v> to node: %v",
			task.Namespace, task.Name, node.Name)
	}

	if success {
		log.InfraLogger.V(6).Infof("Allocation succeeded for task: <%v/%v>", task.Namespace, task.Name)
	} else {
		if fitErrors != nil && fitErrors.HasNodeErrors() {
			taskFitError = fitErrors
		}
		log.InfraLogger.V(6).Infof("Failed statement allocate for task: <%v/%v>", task.Namespace, task.Name)
	}

	return success, taskFitError
}

func allocateTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, isPipelineOnly bool) bool {
	task.NUMAPlacement = ssn.GetNumaPlacement(task, node)

	if task.IsFractionRequest() || task.IsGpuMemoryRequest() {
		return gpu_sharing.AllocateFractionalGPUTaskToNode(ssn, stmt, task, node, isPipelineOnly)
	}

	if taskAllocatable := node.IsTaskAllocatable(task); !isPipelineOnly && taskAllocatable {
		return bindTaskToNode(ssn, stmt, task, node)
	}
	return pipelineTaskToNode(ssn, stmt, task, node, !isPipelineOnly)
}

func bindTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	log.InfraLogger.V(6).Infof("Binding Task <%v/%v> to node <%v>, requires resources: %v",
		task.Namespace, task.Name, node.Name, task.ResReqVector)

	if err := stmt.Allocate(task, node.Name); err != nil {
		log.InfraLogger.Errorf("Failed to bind Task %v on %v in Session %v, err: %v", task.UID, node.Name, ssn.ID, err)
		return false
	}
	return true
}

func pipelineTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, updateTasksIfExistsOnNode bool) bool {
	log.InfraLogger.V(6).Infof("Pipelining Task <%v/%v> to node <%v>, requires resources: %v",
		task.Namespace, task.Name, node.Name, task.ResReqVector)

	if err := stmt.Pipeline(task, node.Name, updateTasksIfExistsOnNode); err != nil {
		log.InfraLogger.V(6).Infof("Failed to pipeline Task %v on %v in Session %v", task.UID, node.Name, ssn.ID)
		return false
	}
	return true
}

func newFailedTaskAllocationError(
	job *podgroup_info.PodGroupInfo,
	unschedulableTask *pod_info.PodInfo,
	numSchedulableTasks int,
	allocationError *common_info.TasksFitErrors,
) common_info.JobFitError {
	if allocationError == nil {
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
		return newPodSchedulingJobFitError(job, fmt.Sprintf("Resources were not found for pod %s/%s due to: %s",
			unschedulableTask.Namespace, unschedulableTask.Name, allocationError.Error()))
	}

	if len(job.GetAllPodSets()) == 1 && taskSubGroup.GetName() == podgroup_info.DefaultSubGroup {
		return newPodSchedulingJobFitError(job,
			fmt.Sprintf("Resources were found for %d pods while %d are required for gang scheduling. "+
				"Additional pods cannot be scheduled due to: %s",
				numSchedulableTasks, taskSubGroup.GetMinAvailable(), allocationError.Error()))
	}
	return newPodSchedulingJobFitError(job,
		fmt.Sprintf("Resources were found for %d pods from all sub-groups while sub-group %s requires %d pods for gang scheduling. "+
			"Additional pods cannot be scheduled in this sub-group due to: %s",
			numSchedulableTasks, taskSubGroup.GetName(), taskSubGroup.GetMinAvailable(), allocationError.Error()))
}

func newPodSchedulingJobFitError(job *podgroup_info.PodGroupInfo, message string) common_info.JobFitError {
	return common_info.NewJobFitError(
		job.Name,
		podgroup_info.DefaultSubGroup,
		job.Namespace,
		podgroup_info.PodSchedulingErrors,
		[]string{message},
	)
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
