/*
Copyright 2017 The Kubernetes Authors.

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

package podgroup_info

import (
	"crypto/sha256"
	"fmt"
	"time"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	OverCapacity        = "OverCapacity"
	PodSchedulingErrors = "PodSchedulingErrors"
	DefaultSubGroup     = "default"
)

type JobRequirement struct {
	GPU      float64
	MilliCPU float64
	Memory   float64
}

type StalenessInfo struct {
	TimeStamp *time.Time
	Stale     bool
}

type PodGroupInfos struct {
	PodGroupInfos []*PodGroupInfo
}

type PodGroupInfo struct {
	UID common_info.PodGroupID

	Name           string
	Namespace      string
	NamespacedName string

	Queue common_info.QueueID

	Priority int32

	JobFitErrors   enginev2alpha2.UnschedulableExplanations
	NodesFitErrors map[common_info.PodID]*common_info.FitErrors

	Allocated *resource_info.Resource

	CreationTimestamp  metav1.Time
	LastStartTimestamp *time.Time
	PodGroup           *enginev2alpha2.PodGroup
	PodGroupUID        types.UID
	SubGroups          map[string]*SubGroupInfo

	StalenessInfo

	schedulingConstraintsSignature common_info.SchedulingConstraintsSignature

	// inner cache
	tasksToAllocate             []*pod_info.PodInfo
	tasksToAllocateInitResource *resource_info.Resource
	PodStatusIndex              map[pod_status.PodStatus]pod_info.PodsMap
	activeAllocatedCount        *int
}

func NewPodGroupInfo(uid common_info.PodGroupID, tasks ...*pod_info.PodInfo) *PodGroupInfo {
	podGroupInfo := &PodGroupInfo{
		UID:       uid,
		Allocated: resource_info.EmptyResource(),

		JobFitErrors:   make(enginev2alpha2.UnschedulableExplanations, 0),
		NodesFitErrors: make(map[common_info.PodID]*common_info.FitErrors),

		PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{},

		StalenessInfo: StalenessInfo{
			TimeStamp: nil,
			Stale:     false,
		},

		SubGroups: map[string]*SubGroupInfo{
			DefaultSubGroup: NewSubGroupInfo(DefaultSubGroup, 1),
		},

		LastStartTimestamp:   nil,
		activeAllocatedCount: ptr.To(0),
	}

	for _, task := range tasks {
		podGroupInfo.AddTaskInfo(task)
	}

	return podGroupInfo
}

func (pgi *PodGroupInfo) GetAllPodsMap() pod_info.PodsMap {
	allPods := pod_info.PodsMap{}
	for _, subGroup := range pgi.SubGroups {
		for podId, podInfo := range subGroup.GetPodInfos() {
			allPods[podId] = podInfo
		}
	}
	return allPods
}

func (pgi *PodGroupInfo) GetSubGroups() map[string]*SubGroupInfo {
	return pgi.SubGroups
}

func (pgi *PodGroupInfo) SetDefaultMinAvailable(minAvailable int32) {
	if pgi.SubGroups == nil {
		pgi.SubGroups = map[string]*SubGroupInfo{}
	}

	if _, exists := pgi.SubGroups[DefaultSubGroup]; !exists {
		pgi.SubGroups[DefaultSubGroup] = NewSubGroupInfo(DefaultSubGroup, 0)
	}
	pgi.SubGroups[DefaultSubGroup].SetMinAvailable(minAvailable)
}

func (pgi *PodGroupInfo) IsPreemptibleJob() bool {
	return pgi.Priority < constants.PriorityBuildNumber
}

func (pgi *PodGroupInfo) SetPodGroup(pg *enginev2alpha2.PodGroup) {
	pgi.Name = pg.Name
	pgi.Namespace = pg.Namespace
	pgi.NamespacedName = fmt.Sprintf("%s/%s", pgi.Namespace, pgi.Name)
	pgi.Queue = common_info.QueueID(pg.Spec.Queue)
	pgi.CreationTimestamp = pg.GetCreationTimestamp()
	pgi.PodGroup = pg
	pgi.PodGroupUID = pg.UID
	pgi.setSubGroups(pg)

	if pg.Annotations[commonconstants.StalePodgroupTimeStamp] != "" {
		staleTimeStamp, err := time.Parse(time.RFC3339, pg.Annotations[commonconstants.StalePodgroupTimeStamp])
		if err != nil {
			log.InfraLogger.V(7).Warnf("Failed to parse stale timestamp for podgroup <%s> err: %v",
				pgi.NamespacedName, err)
		} else {
			pgi.StalenessInfo.TimeStamp = &staleTimeStamp
			pgi.StalenessInfo.Stale = true
		}
	}

	if pg.Annotations[commonconstants.LastStartTimeStamp] != "" {
		startTime, err := time.Parse(time.RFC3339, pg.Annotations[commonconstants.LastStartTimeStamp])
		if err != nil {
			log.InfraLogger.V(7).Warnf("Failed to parse start timestamp for podgroup <%s> err: %v",
				pgi.NamespacedName, err)
		} else {
			pgi.LastStartTimestamp = &startTime
		}
	}

	log.InfraLogger.V(7).Infof(
		"SetPodGroup. podGroupName=<%s>, PodGroupUID=<%s> pgi.PodGroupIndex=<%d>",
		pgi.Name, pgi.PodGroupUID)
}

func (pgi *PodGroupInfo) setSubGroups(podGroup *enginev2alpha2.PodGroup) {
	if len(podGroup.Spec.SubGroups) > 0 {
		pgi.SubGroups = map[string]*SubGroupInfo{}
		for _, sg := range podGroup.Spec.SubGroups {
			subGroupInfo := FromSubGroup(&sg)
			pgi.SubGroups[subGroupInfo.name] = subGroupInfo
		}
		return
	}
	if pgi.SubGroups == nil {
		pgi.SubGroups = map[string]*SubGroupInfo{}
	}
	defaultSubGroup, found := pgi.SubGroups[DefaultSubGroup]
	if !found {
		pgi.SubGroups[DefaultSubGroup] = NewSubGroupInfo(DefaultSubGroup, max(podGroup.Spec.MinMember, 1))
	} else {
		defaultSubGroup.SetMinAvailable(max(podGroup.Spec.MinMember, 1))
	}
}

func (pgi *PodGroupInfo) addTaskIndex(ti *pod_info.PodInfo) {
	if _, found := pgi.PodStatusIndex[ti.Status]; !found {
		pgi.PodStatusIndex[ti.Status] = pod_info.PodsMap{}
	}

	pgi.PodStatusIndex[ti.Status][ti.UID] = ti
	if pod_status.IsActiveAllocatedStatus(ti.Status) {
		pgi.activeAllocatedCount = ptr.To(*pgi.activeAllocatedCount + 1)
	}

	pgi.invalidateTasksCache()
}

func (pgi *PodGroupInfo) AddTaskInfo(ti *pod_info.PodInfo) {
	taskSubGroupName := DefaultSubGroup
	if ti.SubGroupName != "" {
		taskSubGroupName = ti.SubGroupName
	}
	subGroup, found := pgi.SubGroups[taskSubGroupName]
	if !found {
		log.InfraLogger.Warningf("AddTaskInfo for task <%s/%s> of podGroup: <%s/%s>: SubGroup not found <%s>", ti.Namespace, ti.Name, pgi.Namespace, pgi.Name, taskSubGroupName)
		return
	}

	subGroup.AssignTask(ti)
	pgi.addTaskIndex(ti)

	if pod_status.AllocatedStatus(ti.Status) {
		pgi.Allocated.AddResourceRequirements(ti.ResReq)
	}
}

func (pgi *PodGroupInfo) UpdateTaskStatus(task *pod_info.PodInfo, status pod_status.PodStatus) error {
	// Reset the task state
	if err := pgi.resetTaskState(task); err != nil {
		return err
	}

	// Update task's status to the target status
	task.Status = status
	pgi.AddTaskInfo(task)

	return nil
}

func (pgi *PodGroupInfo) deleteTaskIndex(ti *pod_info.PodInfo) {
	if tasks, found := pgi.PodStatusIndex[ti.Status]; found {
		delete(tasks, ti.UID)
		if pod_status.IsActiveAllocatedStatus(ti.Status) {
			pgi.activeAllocatedCount = ptr.To(*pgi.activeAllocatedCount - 1)
		}

		if len(tasks) == 0 {
			delete(pgi.PodStatusIndex, ti.Status)
		}

		pgi.invalidateTasksCache()
	}
}

func (pgi *PodGroupInfo) invalidateTasksCache() {
	pgi.tasksToAllocate = nil
	pgi.tasksToAllocateInitResource = nil
}

func (pgi *PodGroupInfo) GetActiveAllocatedTasksCount() int {
	if pgi.activeAllocatedCount == nil {
		var taskCount int
		for _, task := range pgi.GetAllPodsMap() {
			if pod_status.IsActiveAllocatedStatus(task.Status) {
				taskCount++
			}
		}
		pgi.activeAllocatedCount = ptr.To(taskCount)
	}
	return *pgi.activeAllocatedCount
}

func (pgi *PodGroupInfo) GetActivelyRunningTasksCount() int32 {
	tasksCount := int32(0)
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsActiveUsedStatus(task.Status) {
			tasksCount += 1
		}
	}
	return tasksCount
}

func (pgi *PodGroupInfo) resetTaskState(ti *pod_info.PodInfo) error {
	task, found := pgi.GetAllPodsMap()[ti.UID]
	if !found {
		return fmt.Errorf("failed to find task <%v/%v> in job <%v>",
			ti.Namespace, ti.Name, pgi.NamespacedName)
	}

	if pod_status.AllocatedStatus(task.Status) {
		pgi.Allocated.SubResourceRequirements(task.ResReq)
	}

	pgi.deleteTaskIndex(ti)
	return nil

}

func (pgi *PodGroupInfo) GetNumAliveTasks() int {
	numTasks := 0
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsAliveStatus(task.Status) {
			numTasks += 1
		}
	}
	return numTasks
}

func (pgi *PodGroupInfo) GetNumActiveUsedTasks() int {
	numTasks := 0
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsActiveUsedStatus(task.Status) {
			numTasks += 1
		}
	}
	return numTasks
}

func (pgi *PodGroupInfo) GetNumAllocatedTasks() int {
	numTasks := 0
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.AllocatedStatus(task.Status) {
			numTasks++
		}
	}
	return numTasks
}

func (pgi *PodGroupInfo) GetPendingTasks() []*pod_info.PodInfo {
	var pendingTasks []*pod_info.PodInfo
	for _, task := range pgi.GetAllPodsMap() {
		if task.Status == pod_status.Pending {
			pendingTasks = append(pendingTasks, task)
		}
	}
	return pendingTasks

}

func (pgi *PodGroupInfo) GetNumPendingTasks() int {
	return len(pgi.PodStatusIndex[pod_status.Pending])
}

func (pgi *PodGroupInfo) GetNumGatedTasks() int {
	return len(pgi.PodStatusIndex[pod_status.Gated])
}

func (pgi *PodGroupInfo) GetAliveTasksRequestedGPUs() float64 {
	tasksTotalRequestedGPUs := float64(0)
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsAliveStatus(task.Status) {
			tasksTotalRequestedGPUs += task.ResReq.GPUs()
		}
	}

	return tasksTotalRequestedGPUs
}

func (pgi *PodGroupInfo) GetTasksActiveAllocatedReqResource() *resource_info.Resource {
	tasksTotalRequestedResource := resource_info.EmptyResource()
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			tasksTotalRequestedResource.AddResourceRequirements(task.ResReq)
		}
	}

	return tasksTotalRequestedResource
}

func (pgi *PodGroupInfo) IsReadyForScheduling() bool {
	for _, subGroup := range pgi.GetSubGroups() {
		if !subGroup.IsReadyForScheduling() {
			return false
		}
	}
	return true
}

func (pgi *PodGroupInfo) IsElastic() bool {
	for _, subGroup := range pgi.GetSubGroups() {
		if subGroup.IsElastic() {
			return true
		}
	}
	return false
}

func (pgi *PodGroupInfo) IsStale() bool {
	if pgi.PodStatusIndex[pod_status.Succeeded] != nil {
		return false
	}

	totalActivePods := pgi.GetNumActiveUsedTasks()
	if totalActivePods == 0 {
		return false
	}
	for _, subGroup := range pgi.GetSubGroups() {
		if !subGroup.IsGangSatisfied() {
			return true
		}
	}
	return false
}

func (pgi *PodGroupInfo) IsGangSatisfied() bool {
	for _, subGroup := range pgi.SubGroups {
		if !subGroup.IsGangSatisfied() {
			return false
		}
	}
	return true
}

func (pgi *PodGroupInfo) ShouldPipelineJob() bool {
	for _, subGroup := range pgi.SubGroups {
		hasPipelinedTask := false
		activeAllocatedTasksCount := 0
		for _, task := range subGroup.GetPodInfos() {
			if task.Status == pod_status.Pipelined {
				log.InfraLogger.V(7).Infof("task: <%v/%v> was pipelined to node: <%v>",
					task.Namespace, task.Name, task.NodeName)
				hasPipelinedTask = true
			} else if pod_status.IsActiveAllocatedStatus(task.Status) {
				activeAllocatedTasksCount += 1
			}
		}

		if hasPipelinedTask && activeAllocatedTasksCount < int(subGroup.GetMinAvailable()) {
			log.InfraLogger.V(7).Infof("Subgroup: <%v/%v> has pipelined tasks, and not enough allocated pods for minAvailable <%v>. Pipeline all.",
				pgi.UID, subGroup.GetName(), subGroup.GetMinAvailable())
			return true
		}
	}
	return false
}

func (pgi *PodGroupInfo) Clone() *PodGroupInfo {
	return pgi.CloneWithTasks(maps.Values(pgi.GetAllPodsMap()))
}

func (pgi *PodGroupInfo) CloneWithTasks(tasks []*pod_info.PodInfo) *PodGroupInfo {
	info := &PodGroupInfo{
		UID:       pgi.UID,
		Name:      pgi.Name,
		Namespace: pgi.Namespace,
		Queue:     pgi.Queue,
		Priority:  pgi.Priority,

		Allocated: resource_info.EmptyResource(),

		JobFitErrors:   make(enginev2alpha2.UnschedulableExplanations, 0),
		NodesFitErrors: make(map[common_info.PodID]*common_info.FitErrors),

		PodGroup:    pgi.PodGroup,
		PodGroupUID: pgi.PodGroupUID,
		SubGroups:   map[string]*SubGroupInfo{},

		PodStatusIndex:       map[pod_status.PodStatus]pod_info.PodsMap{},
		activeAllocatedCount: ptr.To(0),
	}

	pgi.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	for _, subGroup := range pgi.SubGroups {
		info.SubGroups[subGroup.name] = NewSubGroupInfo(subGroup.name, subGroup.minAvailable)
	}

	for _, task := range tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

func (pgi *PodGroupInfo) String() string {
	res := ""

	for _, subGroup := range pgi.GetSubGroups() {
		res = res + fmt.Sprintf("\t\t subGroup %s: minAvailable(%v)\n",
			subGroup.name, subGroup.minAvailable)
	}

	i := 0
	for _, task := range pgi.GetAllPodsMap() {
		res = res + fmt.Sprintf("\n\t task %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): namespace %v (%v), name %v, podGroup %+v",
		pgi.UID, pgi.Namespace, pgi.Queue, pgi.Name, pgi.PodGroup) + res
}

func (pgi *PodGroupInfo) SetTaskFitError(task *pod_info.PodInfo, fitErrors *common_info.FitErrors) {
	existingFitErrors, found := pgi.NodesFitErrors[task.UID]
	if found {
		existingFitErrors.AddNodeErrors(fitErrors)
	} else {
		pgi.NodesFitErrors[task.UID] = fitErrors
	}
}

func (pgi *PodGroupInfo) SetJobFitError(reason enginev2alpha2.UnschedulableReason, message string, details *enginev2alpha2.UnschedulableExplanationDetails) {
	pgi.JobFitErrors = append(pgi.JobFitErrors, enginev2alpha2.UnschedulableExplanation{
		Reason:  reason,
		Message: message,
		Details: details,
	})
}

func (pgi *PodGroupInfo) GetSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if pgi.schedulingConstraintsSignature != "" {
		return pgi.schedulingConstraintsSignature
	}

	key := pgi.generateSchedulingConstraintsSignature()

	pgi.schedulingConstraintsSignature = key
	return key
}

func (pgi *PodGroupInfo) generateSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if pgi == nil || pgi.PodGroup == nil {
		return ""
	}

	hash := sha256.New()

	topologyConstraintSignature := generateTopologyConstraintsSignature(&pgi.PodGroup.Spec.TopologyConstraint)
	hash.Write([]byte(topologyConstraintSignature))

	var signatures []common_info.SchedulingConstraintsSignature
	for _, pod := range pgi.GetAllPodsMap() {
		if pod_status.IsActiveAllocatedStatus(pod.Status) {
			continue
		}

		key := pod.GetSchedulingConstraintsSignature()
		signatures = append(signatures, key)
	}
	slices.Sort(signatures)

	for _, signature := range signatures {
		hash.Write([]byte(signature))
	}

	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}

func (jr *JobRequirement) Get(resourceName v1.ResourceName) float64 {
	switch resourceName {
	case v1.ResourceCPU:
		return jr.MilliCPU
	case v1.ResourceMemory:
		return jr.Memory
	case resource_info.GPUResourceName:
		return jr.GPU
	default:
		return 0
	}
}

func generateTopologyConstraintsSignature(topologyConstraint *enginev2alpha2.TopologyConstraint) common_info.SchedulingConstraintsSignature {
	hash := sha256.New()
	hash.Write([]byte(fmt.Sprintf("%s:%s:%s", topologyConstraint.Topology, topologyConstraint.RequiredTopologyLevel, topologyConstraint.PreferredTopologyLevel)))
	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}
