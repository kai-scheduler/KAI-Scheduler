/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"

	"golang.org/x/exp/slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type Statement struct {
	operations []Operation
	ssn        *Session
	sessionID  string
}

type Checkpoint int

func (s *Statement) Checkpoint() Checkpoint {
	return Checkpoint(len(s.operations))
}

func (s *Statement) Rollback(cp Checkpoint) error {
	if cp < 0 || int(cp) > len(s.operations) {
		return fmt.Errorf("invalid checkpoint %d, statement has %d operations", cp, len(s.operations))
	}

	for i := len(s.operations) - 1; i >= int(cp); i-- {
		if err := s.undoOperation(i); err != nil {
			return fmt.Errorf("failed to rollback operation %d: %v", i, err)
		}
	}

	s.operations = s.operations[:cp]
	return nil
}

func (s *Statement) Evict(reclaimeeTask *pod_info.PodInfo, message string,
	evictionMetadata eviction_info.EvictionMetadata) error {
	// Update status in session
	job, jobFound := s.ssn.ClusterInfo.PodGroupInfos[reclaimeeTask.Job]
	if !jobFound {
		log.InfraLogger.Errorf("Failed to find Job <%s> in session <%s>",
			reclaimeeTask.Job, s.sessionID)
		return fmt.Errorf("failed to find job <%s> in session", reclaimeeTask.Job)
	}

	node, nodeFound := s.ssn.ClusterInfo.Nodes[reclaimeeTask.NodeName]
	if !nodeFound {
		log.InfraLogger.Errorf("Failed to find node: %v", reclaimeeTask.NodeName)
		return fmt.Errorf("node doesn't exist in sesssion: <%s>", reclaimeeTask.NodeName)
	}

	previousStatus := reclaimeeTask.Status
	previousGpuGroup := reclaimeeTask.GPUGroups
	previousIsVirtualStatus := reclaimeeTask.IsVirtualStatus
	var previousResourceClaimInfo bindrequest_info.ResourceClaimInfo
	if reclaimeeTask.ResourceClaimInfo != nil {
		previousResourceClaimInfo = reclaimeeTask.ResourceClaimInfo.Clone()
	}

	if err := job.UpdateTaskStatus(reclaimeeTask, pod_status.Releasing); err != nil {
		log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
			reclaimeeTask.Namespace, reclaimeeTask.Name, pod_status.Releasing, s.sessionID, err)
		return fmt.Errorf("failed to update task status for <%v/%v>", reclaimeeTask.Namespace, reclaimeeTask.Name)
	}
	if err := node.UpdateTask(reclaimeeTask); err != nil {
		log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
			reclaimeeTask.Namespace, reclaimeeTask.Name, pod_status.Releasing, s.sessionID, err)
		return fmt.Errorf("failed to update task <%v/%v>", reclaimeeTask.Namespace, reclaimeeTask.Name)
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: reclaimeeTask,
			})
		}
	}

	s.operations = append(s.operations,
		evictOperation{
			taskInfo:          reclaimeeTask,
			previousStatus:    previousStatus,
			previousNode:      node,
			previousGpuGroups: previousGpuGroup,
			message:           message,
			evictionMetadata:  evictionMetadata,
			reverseOperation: func() error {
				return s.unevict(reclaimeeTask, previousStatus, node, previousGpuGroup, previousResourceClaimInfo, previousIsVirtualStatus)
			},
		},
	)
	reclaimeeTask.IsVirtualStatus = true

	log.InfraLogger.V(6).Infof("Statement evicted task: <%v/%v> from node: <%v>",
		reclaimeeTask.Namespace, reclaimeeTask.Name, node.Name)

	return nil
}

func (s *Statement) commitEvict(reclaimee *pod_info.PodInfo, evictOp evictOperation) error {
	reclaimeePodGroup, found := s.ssn.ClusterInfo.PodGroupInfos[reclaimee.Job]
	if !found {
		return fmt.Errorf("could not reclaim pod <%v/%v> because could not find its podGroup <%v>",
			reclaimee.Namespace, reclaimee.Name, reclaimee.Job)
	}

	previousStatus := reclaimee.Status
	previousGpuGroup := reclaimee.GPUGroups
	previousResourceClaimInfo := reclaimee.ResourceClaimInfo
	previousIsVirtualStatus := reclaimee.IsVirtualStatus
	if err := s.ssn.Cache.Evict(reclaimee.Pod, reclaimeePodGroup, evictOp.evictionMetadata, evictOp.message); err != nil {
		log.InfraLogger.Errorf("Failed to evict task <%v/%v>: %v.", reclaimee.Namespace, reclaimee.Name, err)
		if e := s.unevict(reclaimee, previousStatus, evictOp.previousNode, previousGpuGroup, previousResourceClaimInfo,
			previousIsVirtualStatus); e != nil {
			log.InfraLogger.Errorf("Failed to un-evict task <%v/%v>: %v.",
				reclaimee.Namespace, reclaimee.Name, e)
		}
		return err
	}
	reclaimee.IsVirtualStatus = false

	return nil
}

func (s *Statement) unevict(
	reclaimee *pod_info.PodInfo, previousStatus pod_status.PodStatus, node *node_info.NodeInfo,
	previousGpuGroups []string, previousResourceClaimInfo bindrequest_info.ResourceClaimInfo, previousIsVirtualStatus bool) error {
	// Update status in session
	job, found := s.ssn.ClusterInfo.PodGroupInfos[reclaimee.Job]
	if found {
		if err := job.UpdateTaskStatus(reclaimee, previousStatus); err != nil {
			log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, pod_status.Releasing, s.sessionID, err)
		}
	} else {
		log.InfraLogger.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			reclaimee.Job, s.sessionID)
	}
	reclaimee.GPUGroups = previousGpuGroups
	reclaimee.IsVirtualStatus = previousIsVirtualStatus
	reclaimee.ResourceClaimInfo = previousResourceClaimInfo.Clone()

	// Update task in node.
	if node != nil {
		key := pod_info.PodKey(reclaimee.Pod)
		if _, found := node.PodInfos[key]; found {
			if err := node.UpdateTask(reclaimee); err != nil {
				log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
					reclaimee.Namespace, reclaimee.Name, pod_status.Releasing, s.sessionID, err)
			}
		} else if err := node.AddTask(reclaimee); err != nil {
			// This can happen if job was 1st evicted, then pipelined -> it will cause the job to not be in the node
			log.InfraLogger.Errorf("Failed to add task <%v/%v> status to %v in Session <%v>: %v",
				reclaimee.Namespace, reclaimee.Name, pod_status.Releasing, s.sessionID, err)
		}
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: reclaimee,
			})
		}
	}

	return nil
}

func (s *Statement) Pipeline(task *pod_info.PodInfo, hostname string, updateTaskIfExistsOnNode bool) error {
	// Only update status in session
	job, foundJob := s.ssn.ClusterInfo.PodGroupInfos[task.Job]
	node, foundNode := s.ssn.ClusterInfo.Nodes[hostname]
	if !foundNode || !foundJob {
		log.InfraLogger.Errorf("Failed to find Node <%s> or job: <%s> in Session <%s> index when binding.",
			hostname, task.Job, s.sessionID)
		return fmt.Errorf("failed to find node: <%v> or job: <%v>", hostname, task.Job)
	}

	taskKey := pod_info.PodKey(task.Pod)
	taskOnNode, foundOnNode := node.PodInfos[taskKey]
	isSharedAndMoveToDifferentGPU := false
	if foundOnNode {
		isSharedAndMoveToDifferentGPU = len(task.GPUGroups) > 0 && task.IsSharedGPUAllocation() &&
			!slices.Equal(task.GPUGroups, []string{"-1"}) && !slices.Equal(task.GPUGroups, taskOnNode.GPUGroups)
	}

	// If the task already exist on the node, we assume it was evicted before.
	// If task already exist on the node, and we didn't ask to update if on the node,
	// and there is no special reason we should update on the node, then we need to unevict instead of pipelining it.
	if foundOnNode && !updateTaskIfExistsOnNode && !isSharedAndMoveToDifferentGPU {
		task.GPUGroups = taskOnNode.GPUGroups
		log.InfraLogger.V(6).Infof("Task: <%v/%v> already exists on node: <%v>, unevicting it", task.Namespace, task.Name, hostname)
		if err := s.Unevict(task); err != nil {
			log.InfraLogger.Errorf("Failed to unevict task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.sessionID, err)
			return err
		}
		return nil
	}

	previousStatus := task.Status
	if err := job.UpdateTaskStatus(task, pod_status.Pipelined); err != nil {
		log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
			task.Namespace, task.Name, pod_status.Pipelined, s.sessionID, err)
	}

	previousNode := task.NodeName
	task.NodeName = hostname
	previousGpuGroup := task.GPUGroups
	previousIsVirtualStatus := task.IsVirtualStatus
	var previousResourceClaimInfo bindrequest_info.ResourceClaimInfo
	if task.ResourceClaimInfo != nil {
		previousResourceClaimInfo = task.ResourceClaimInfo.Clone()
	}

	if isSharedAndMoveToDifferentGPU {
		log.InfraLogger.V(6).Infof(
			"Task: <%v/%v> already exists on node: <%v> on gpu index of: <%v>, moving it to index: <%v>",
			task.Namespace, task.Name, hostname, taskOnNode.GPUGroups, task.GPUGroups)
		previousGpuGroup = taskOnNode.GPUGroups
		if err := node.ConsolidateSharedPodInfoToDifferentGPU(task); err != nil {
			log.InfraLogger.Errorf("Failed to unevict task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.sessionID, err)
			return err
		}
	} else if foundOnNode {
		if err := node.UpdateTask(task); err != nil {
			log.InfraLogger.Errorf("Failed to update task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.sessionID, err)
		}
	} else if err := node.AddTask(task); err != nil {
		log.InfraLogger.Errorf("Failed to pipeline task <%v/%v> to node <%v> in Session <%v>: %v",
			task.Namespace, task.Name, hostname, s.sessionID, err)
		return err
	}

	log.InfraLogger.V(6).Infof("After pipelined Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
		task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)

	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	s.operations = append(s.operations, pipelineOperation{
		taskInfo:                  task,
		previousStatus:            previousStatus,
		previousNode:              previousNode,
		previousGpuGroups:         previousGpuGroup,
		previousResourceClaimInfo: previousResourceClaimInfo,
		nextNode:                  hostname,
		message:                   fmt.Sprintf("Pod %s/%s was pipelined to node %s", task.Namespace, task.Name, node.Name),
		reverseOperation: func() error {
			return s.unpipeline(task, previousNode, previousStatus, previousGpuGroup, previousResourceClaimInfo, previousIsVirtualStatus)
		},
	})
	task.IsVirtualStatus = true

	log.InfraLogger.V(6).Infof(
		"Statement pipelined task: <%v/%v> to node: <%v>, gpuGroup: <%v>",
		task.Namespace, task.Name, hostname, task.GPUGroups)

	return nil
}

func (s *Statement) Allocate(task *pod_info.PodInfo, hostname string) error {
	node := s.ssn.ClusterInfo.Nodes[hostname]

	// Only update status in session
	job, found := s.ssn.ClusterInfo.PodGroupInfos[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, pod_status.Allocated); err != nil {
			log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, pod_status.Allocated, s.sessionID, err)
			return err
		}
	} else {
		log.InfraLogger.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, s.sessionID)
		return fmt.Errorf("failed to find job %s", task.Job)
	}

	task.NodeName = hostname

	if node, found := s.ssn.ClusterInfo.Nodes[hostname]; found {
		if err := node.AddTask(task); err != nil {
			log.InfraLogger.Errorf("Failed to add task <%v/%v> to node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.sessionID, err)
			return err
		}
		log.InfraLogger.V(5).Infof(
			"After allocated Task <%v/%v> to Node <%v>: idle <%v>, used <%v>, releasing <%v>",
			task.Namespace, task.Name, node.Name, node.Idle, node.Used, node.Releasing)
	} else {
		log.InfraLogger.Errorf("Failed to find Node <%s> in Session <%s> index when binding.",
			hostname, s.sessionID)
		return fmt.Errorf("failed to find node %s", hostname)
	}

	// Callbacks
	for _, eh := range s.ssn.eventHandlers {
		if eh.AllocateFunc != nil {
			eh.AllocateFunc(&Event{
				Task: task,
			})
		}
	}

	// Update status in session
	previousIsVirtualStatus := task.IsVirtualStatus
	s.operations = append(s.operations,
		allocateOperation{
			taskInfo: task.Clone(),
			nextNode: node.Name,
			reverseOperation: func() error {
				return s.unallocate(task, node.Name, previousIsVirtualStatus)
			},
		},
	)
	task.IsVirtualStatus = true

	log.InfraLogger.V(6).Infof(
		"Statement allocated task: <%v/%v> to node: <%v>",
		task.Namespace, task.Name, hostname)

	return nil
}

func (s *Statement) commitAllocate(task *pod_info.PodInfo) error {
	hostname := task.NodeName
	node, found := s.ssn.ClusterInfo.Nodes[hostname]
	if !found {
		log.InfraLogger.Errorf("Failed to find node: %v", hostname)
		return fmt.Errorf("node doesn't exist on cluster")
	}

	var err error
	defer func() {
		if err != nil {
			s.cleanupFailedAllocation(task, node)
		}
	}()

	if task.IsFractionAllocation() {
		for _, gpuGroup := range task.GPUGroups {
			if _, found := node.UsedSharedGPUsMemory[gpuGroup]; !found {
				node.UsedSharedGPUsMemory[gpuGroup] = 0
			}
		}
	}

	if err = s.ssn.BindPod(task); err != nil {
		log.InfraLogger.Errorf("Failed to bind task <%v/%v>. Error: %v",
			task.Namespace, task.Name, err)
	}

	return err
}

// unallocate the pod for task
func (s *Statement) unallocate(task *pod_info.PodInfo, previousNodeName string, previousIsVirtualStatus bool) error {
	// Update status in session
	job, found := s.ssn.ClusterInfo.PodGroupInfos[task.Job]
	if found {
		if err := job.UpdateTaskStatus(task, pod_status.Pending); err != nil {
			log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, pod_status.Pending, s.sessionID, err)
		}
	} else {
		log.InfraLogger.Errorf("Failed to find Job <%s> in Session <%s> index when unallocating.",
			task.Job, s.sessionID)
	}

	if node, found := s.ssn.ClusterInfo.Nodes[task.NodeName]; found {
		log.InfraLogger.V(6).Infof("Remove Task <%v> from node <%v>", task.Name, task.NodeName)
		err := node.RemoveTask(task)
		if err != nil {
			log.InfraLogger.Errorf("Failed to remove Task <%v> on node <%v>: %s", task.Name, task.NodeName, err.Error())
		}
	} else {
		log.InfraLogger.Errorf("Failed to find node: %v", previousNodeName)
		return fmt.Errorf("node doesn't exist on cluster")
	}

	task.NodeName = ""
	task.IsVirtualStatus = previousIsVirtualStatus

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: task,
			})
		}
	}
	return nil
}

func (s *Statement) commitPipeline(task *pod_info.PodInfo, message string) {
	s.ssn.Cache.TaskPipelined(task, message)
}

func (s *Statement) unpipeline(
	task *pod_info.PodInfo, previousNode string, previousStatus pod_status.PodStatus, previousGpuGroups []string,
	previousResourceClaimInfo bindrequest_info.ResourceClaimInfo,
	previousIsVirtualStatus bool) error {
	// Only update status in session
	job, found := s.ssn.ClusterInfo.PodGroupInfos[task.Job]
	if found {
		statusToRevertTo := previousStatus
		if err := job.UpdateTaskStatus(task, statusToRevertTo); err != nil {
			log.InfraLogger.Errorf("Failed to update task <%v/%v> status to %v in Session <%v>: %v",
				task.Namespace, task.Name, statusToRevertTo, s.sessionID, err)
		}
	} else {
		log.InfraLogger.Errorf("Failed to find Job <%s> in Session <%s> index when binding.",
			task.Job, s.sessionID)
	}

	hostname := task.NodeName
	task.NodeName = previousNode
	task.GPUGroups = previousGpuGroups
	task.ResourceClaimInfo = previousResourceClaimInfo.Clone()
	task.IsVirtualStatus = previousIsVirtualStatus

	if node, found := s.ssn.ClusterInfo.Nodes[hostname]; found {
		if err := node.RemoveTask(task); err != nil {
			log.InfraLogger.Errorf("Failed to unpipeline task <%v/%v> from node <%v> in Session <%v>: %v",
				task.Namespace, task.Name, hostname, s.sessionID, err)
		}
	} else {
		log.InfraLogger.Errorf("Failed to find Node <%s> in Session <%s> index when binding.",
			hostname, s.sessionID)
		return fmt.Errorf("node doesnt exist on cluster")
	}

	for _, eh := range s.ssn.eventHandlers {
		if eh.DeallocateFunc != nil {
			eh.DeallocateFunc(&Event{
				Task: task,
			})
		}
	}

	return nil
}

func (s *Statement) Unevict(taskToUnevict *pod_info.PodInfo) error {
	log.InfraLogger.V(6).Infof("Unevicting task: %v", taskToUnevict.Name)
	return s.undoEarliestValidOperation(taskToUnevict, evict)
}

func (s *Statement) ConvertAllAllocatedToPipelined(jobID common_info.PodGroupID) error {
	for _, op := range s.operations {
		currentTaskInOperations := op.TaskInfo()

		if currentTaskInOperations.Job != jobID {
			continue
		}
		if op.Name() != allocate {
			continue
		}
		allocateOp := op.(allocateOperation)

		nodeName := currentTaskInOperations.NodeName
		err := s.unallocate(currentTaskInOperations, allocateOp.nextNode, true)
		if err != nil {
			return err
		}

		err = s.Pipeline(currentTaskInOperations, nodeName, true)
		if err != nil {
			return err
		}
	}

	var newOperations []Operation
	for _, op := range s.operations {
		if !(op.TaskInfo().Job == jobID && op.Name() == allocate) {
			newOperations = append(newOperations, op)
		}
	}
	s.operations = newOperations

	return nil
}

func (s *Statement) clearOperations() {
	s.operations = []Operation{}
}

func (s *Statement) Discard() {
	if len(s.operations) == 0 {
		// don't even print the info message
		return
	}

	log.InfraLogger.V(6).Infof("Discarding operations ...")
	for i := len(s.operations) - 1; i >= 0; i-- {
		_ = s.undoOperation(i)
	}

	s.clearOperations()
}

func (s *Statement) Commit() error {
	if len(s.operations) == 0 {
		// don't even print the info message
		return nil
	}

	var err error

	log.InfraLogger.V(4).Infof("Committing operations ...")
	for i, op := range s.operations {
		if !s.operationValid(i) {
			continue
		}

		taskInfo := op.TaskInfo()
		switch op.Name() {
		case evict:
			log.InfraLogger.V(4).Infof("Evicting task: %v/%v", taskInfo.Namespace, taskInfo.Name)
			evictOp := op.(evictOperation)
			if err = s.commitEvict(taskInfo, evictOp); err != nil {
				log.InfraLogger.Errorf("Failed to evict task <%v/%v>, error: <%v>",
					taskInfo.Namespace, taskInfo.Name, err)
			}
		case pipeline:
			log.InfraLogger.V(4).Infof("Pipelining task: %v/%v", taskInfo.Namespace, taskInfo.Name)
			s.commitPipeline(taskInfo, op.(pipelineOperation).message)
		case allocate:
			log.InfraLogger.V(4).Infof("Allocating task: %v/%v", taskInfo.Namespace, taskInfo.Name)
			err = s.commitAllocate(taskInfo)
			if err != nil {
				log.InfraLogger.Errorf("Failed to allocate task. error: %s", err.Error())
				s.clearOperations()
				return err
			}
		}
	}

	s.clearOperations()

	return err
}

// undoEarliestValidOperation will undo the earliest valid operation of the given type
func (s *Statement) undoEarliestValidOperation(taskToUndo *pod_info.PodInfo, opName string) error {
	for index, op := range s.operations {
		// If the operation was already undone, skip it
		if !s.operationValid(index) {
			continue
		}

		if taskToUndo.UID != op.TaskInfo().UID || opName != op.Name() {
			continue
		}

		err := s.undoOperation(index)
		if err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("no operation of type %s found for task %s/%s", opName, taskToUndo.Namespace, taskToUndo.Name)
}

func (s *Statement) undoOperation(index int) error {
	if !s.operationValid(index) {
		return nil
	}

	op := s.operations[index]

	taskToUndo := op.TaskInfo()
	var err error
	var redoOperation ReverseOperation
	switch op.(type) {
	case evictOperation:
		operation := op.(evictOperation)
		redoOperation = func() error {
			return s.Evict(taskToUndo, operation.message, operation.evictionMetadata)
		}
	case pipelineOperation:
		operation := op.(pipelineOperation)
		redoOperation = func() error {
			return s.Pipeline(taskToUndo, operation.nextNode, true)
		}
	case allocateOperation:
		redoOperation = func() error {
			return s.Allocate(taskToUndo, op.(allocateOperation).nextNode)
		}
	case undoOperation:
		redoOperation = func() error {
			return s.undoOperation(op.(undoOperation).operationIndex)
		}
	}

	if err = op.Reverse(); err != nil {
		return fmt.Errorf("failed to reverse operation %s: %v", op.Name(), err)
	}

	s.operations = append(s.operations,
		undoOperation{
			operationIndex:   index,
			reverseOperation: redoOperation,
		},
	)

	return err
}

func (s *Statement) cleanupFailedAllocation(task *pod_info.PodInfo, node *node_info.NodeInfo) {
	log.InfraLogger.V(4).Infof("Cleaning up for failed allocation for task: <%v/%v> on node: %v",
		task.Namespace, task.Name, node.Name)

	_ = s.unallocate(task, node.Name, false)
}

func (s *Statement) operationValid(i int) bool {
	for undoIndex, operation := range s.operations {
		if operation.Name() != undo {
			continue
		}
		if operation.(undoOperation).operationIndex == i {
			return !s.operationValid(undoIndex)
		}
	}
	return true
}
