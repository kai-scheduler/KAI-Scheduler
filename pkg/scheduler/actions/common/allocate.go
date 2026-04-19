// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/gpu_sharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

func AllocateJob(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	job *podgroup_info.PodGroupInfo, isPipelineOnly bool) bool {
	tasksToAllocate := podgroup_info.GetTasksToAllocate(job, ssn.SubGroupOrderFn, ssn.TaskOrderFn, !isPipelineOnly)

	result := ssn.IsJobOverQueueCapacityFn(job, tasksToAllocate)
	if !result.IsSchedulable {
		if !isPipelineOnly {
			job.SetJobFitError(result.Reason, result.Message, result.Details)
		}
		return false
	}

	defer ssn.CleanAllocationAttemptCache(job)
	cp := stmt.Checkpoint()
	for index, task := range tasksToAllocate {
		success := allocateTask(ssn, stmt, nodes, task, isPipelineOnly)
		if !success {
			if err := stmt.Rollback(cp); err != nil {
				log.InfraLogger.Errorf("Failed to rollback statement in session %v, err: %v", ssn.UID, err)
			}

			handleFailedTaskAllocation(job, task, index)
			return false
		}
	}

	return true
}

func allocateTask(ssn *framework.Session, stmt *framework.Statement, nodes []*node_info.NodeInfo,
	task *pod_info.PodInfo, isPipelineOnly bool) (success bool) {
	job := ssn.PodGroupInfos[task.Job]
	err := ssn.PrePredicateFn(task, job)
	if err != nil {
		log.InfraLogger.V(6).Infof("pre-predicates failed on task %s/%s. Error: %v",
			task.Namespace, task.Name, err)

		fitErrors := common_info.NewFitErrors()
		fitErrors.SetError(err.Error())
		job.SetTaskFitError(task, fitErrors)
		return false
	}

	log.InfraLogger.V(6).Infof("Looking for best node for task - Task: <%s/%s>, init requested: <%v>.",
		task.Namespace, task.Name, task.ResReq)

	orderedNodes := ssn.OrderedNodesByTask(nodes, task)
	for _, node := range orderedNodes {
		if !ssn.FittingNode(task, node, !isPipelineOnly) {
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
		log.InfraLogger.V(6).Infof("Failed statement allocate for task: <%v/%v>", task.Namespace, task.Name)
	}

	return success
}

func allocateTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, isPipelineOnly bool) bool {
	if task.IsFractionRequest() || task.IsMemoryRequest() {
		return gpu_sharing.AllocateFractionalGPUTaskToNode(ssn, stmt, task, node, isPipelineOnly)
	}

	if taskAllocatable := node.IsTaskAllocatable(task); !isPipelineOnly && taskAllocatable {
		return bindTaskToNode(ssn, stmt, task, node)
	}
	return pipelineTaskToNode(ssn, stmt, task, node, !isPipelineOnly)
}

func bindTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo) bool {
	log.InfraLogger.V(6).Infof("Binding Task <%v/%v> to node <%v>, requires: %v GPUs",
		task.Namespace, task.Name, node.Name, task.ResReq)

	if err := stmt.Allocate(task, node.Name); err != nil {
		log.InfraLogger.Errorf("Failed to bind Task %v on %v in Session %v, err: %v", task.UID, node.Name, ssn.UID, err)
		return false
	}
	return true
}

func pipelineTaskToNode(ssn *framework.Session, stmt *framework.Statement, task *pod_info.PodInfo, node *node_info.NodeInfo, updateTasksIfExistsOnNode bool) bool {
	log.InfraLogger.V(6).Infof("Pipelining Task <%v/%v> to node <%v> requires: %v GPUs",
		task.Namespace, task.Name, node.Name, task.ResReq)

	if err := stmt.Pipeline(task, node.Name, updateTasksIfExistsOnNode); err != nil {
		log.InfraLogger.V(6).Infof("Failed to pipeline Task %v on %v in Session %v", task.UID, node.Name, ssn.UID)
		return false
	}
	return true
}

func handleFailedTaskAllocation(job *podgroup_info.PodGroupInfo, unschedulableTask *pod_info.PodInfo, numSchedulableTasks int) {
	allocationError, found := job.NodesFitErrors[unschedulableTask.UID]

	if !found {
		allocationError = common_info.NewFitErrors()
		allocationError.SetError(common_info.DefaultPodError)
	}

	gangScheduling := isGangScheduling(job)
	taskSubGroupName := podgroup_info.DefaultSubGroup
	if len(unschedulableTask.SubGroupName) != 0 {
		taskSubGroupName = unschedulableTask.SubGroupName
	}
	taskSubGroup := job.GetSubGroups()[taskSubGroupName]

	if !gangScheduling || taskSubGroup.GetNumActiveUsedTasks() >= int(taskSubGroup.GetMinAvailable()) {
		job.SetJobFitError(
			podgroup_info.PodSchedulingErrors,
			fmt.Sprintf("Resources were not found for pod %s/%s due to: %s",
				unschedulableTask.Namespace, unschedulableTask.Name, allocationError.Error()),
			nil)
		return
	}

	if len(job.GetSubGroups()) == 1 && taskSubGroup.GetName() == podgroup_info.DefaultSubGroup {
		job.SetJobFitError(
			podgroup_info.PodSchedulingErrors,
			fmt.Sprintf("Resources were found for %d pods while %d are required for gang scheduling. "+
				"Additional pods cannot be scheduled due to: %s",
				numSchedulableTasks, taskSubGroup.GetMinAvailable(), allocationError.Error()),
			nil)
		return
	}
	job.SetJobFitError(
		podgroup_info.PodSchedulingErrors,
		fmt.Sprintf("Resources were found for %d pods from all sub-groups while sub-group %s requires %d pods for gang scheduling. "+
			"Additional pods cannot be scheduled in this sub-group due to: %s",
			numSchedulableTasks, taskSubGroup.GetName(), taskSubGroup.GetMinAvailable(), allocationError.Error()),
		nil)
}

func isGangScheduling(job *podgroup_info.PodGroupInfo) bool {
	for _, subGroup := range job.GetSubGroups() {
		if subGroup.GetMinAvailable() > 1 {
			return true
		}
	}
	return false
}
