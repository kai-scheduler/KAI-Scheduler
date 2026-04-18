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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	OverCapacity        = "OverCapacity"
	PodSchedulingErrors = "PodSchedulingErrors"
	DefaultSubGroup     = "default"
)

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

	Priority       int32
	Preemptibility enginev2alpha2.Preemptibility

	JobFitErrors   []common_info.JobFitError
	TasksFitErrors map[common_info.PodID]*common_info.TasksFitErrors

	AllocatedVector resource_info.ResourceVector
	VectorMap       *resource_info.ResourceVectorMap

	CreationTimestamp  metav1.Time
	LastStartTimestamp *time.Time
	PodGroup           *enginev2alpha2.PodGroup
	PodGroupUID        types.UID

	RootSubGroupSet *subgroup_info.SubGroupSet
	PodSets         map[string]*subgroup_info.PodSet

	StalenessInfo

	schedulingConstraintsSignature common_info.SchedulingConstraintsSignature

	// inner cache
	allPodsMap                        *pod_info.PodsMap
	tasksToAllocate                   []*pod_info.PodInfo
	tasksToAllocateInitResourceVector resource_info.ResourceVector
	PodStatusIndex                    map[pod_status.PodStatus]pod_info.PodsMap
	activeAllocatedCount              *int
}

func NewPodGroupInfo(uid common_info.PodGroupID, tasks ...*pod_info.PodInfo) *PodGroupInfo {
	return NewPodGroupInfoWithVectorMap(uid, resource_info.NewResourceVectorMap(), tasks...)
}

func NewPodGroupInfoWithVectorMap(uid common_info.PodGroupID, vectorMap *resource_info.ResourceVectorMap, tasks ...*pod_info.PodInfo) *PodGroupInfo {
	defaultSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	defaultSubGroupSet.AddPodSet(subgroup_info.NewPodSet(DefaultSubGroup, 1, nil))

	podGroupInfo := &PodGroupInfo{
		UID:             uid,
		AllocatedVector: resource_info.NewResourceVector(vectorMap),
		VectorMap:       vectorMap,

		JobFitErrors:   make([]common_info.JobFitError, 0),
		TasksFitErrors: make(map[common_info.PodID]*common_info.TasksFitErrors),

		PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{},

		StalenessInfo: StalenessInfo{
			TimeStamp: nil,
			Stale:     false,
		},
		RootSubGroupSet: defaultSubGroupSet,
		PodSets:         defaultSubGroupSet.GetDescendantPodSets(),

		LastStartTimestamp:   nil,
		activeAllocatedCount: ptr.To(0),
	}

	for _, task := range tasks {
		podGroupInfo.AddTaskInfo(task)
	}

	return podGroupInfo
}

func (pgi *PodGroupInfo) GetAllPodsMap() pod_info.PodsMap {
	if pgi.allPodsMap != nil {
		return *pgi.allPodsMap
	}
	totalPods := 0
	for _, podSet := range pgi.PodSets {
		totalPods += len(podSet.GetPodInfos())
	}

	allPods := make(pod_info.PodsMap, totalPods)
	for _, subGroup := range pgi.PodSets {
		for podId, podInfo := range subGroup.GetPodInfos() {
			allPods[podId] = podInfo
		}
	}
	pgi.allPodsMap = &allPods
	return allPods
}

func (pgi *PodGroupInfo) GetAllAllocatedPods() []*pod_info.PodInfo {
	podsMap := pgi.GetAllPodsMap()
	allocated := make([]*pod_info.PodInfo, 0, len(podsMap))
	for _, task := range podsMap {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			allocated = append(allocated, task)
		}
	}
	return allocated
}

func (pgi *PodGroupInfo) GetAllPodSets() map[string]*subgroup_info.PodSet {
	return pgi.PodSets
}

func (pgi *PodGroupInfo) IsPreemptibleJob() bool {
	return pgi.Preemptibility == enginev2alpha2.Preemptible
}

func (pgi *PodGroupInfo) SetPodGroup(pg *enginev2alpha2.PodGroup) {
	pgi.Name = pg.Name
	pgi.Namespace = pg.Namespace
	pgi.NamespacedName = fmt.Sprintf("%s/%s", pgi.Namespace, pgi.Name)
	pgi.Queue = common_info.QueueID(pg.Spec.Queue)
	pgi.CreationTimestamp = pg.GetCreationTimestamp()
	pgi.PodGroup = pg
	pgi.PodGroupUID = pg.UID
	err := pgi.setSubGroups(pg)
	if err != nil {
		log.InfraLogger.V(7).Warnf("Failed to set subgroups for podgroup <%s> err: %v",
			pg.Namespace, pg.Name)
	}

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

func (pgi *PodGroupInfo) setSubGroups(podGroup *enginev2alpha2.PodGroup) error {
	rootSubGroupSet, err := subgroup_info.FromPodGroup(podGroup)
	if err != nil {
		return err
	}
	pgi.RootSubGroupSet = rootSubGroupSet
	podSets := rootSubGroupSet.GetDescendantPodSets()
	if len(podSets) > 0 {
		pgi.PodSets = podSets
	} else {
		if defaultPodSet, found := pgi.PodSets[DefaultSubGroup]; found {
			minAvail := int32(1)
			if podGroup.Spec.MinMember != nil {
				minAvail = max(*podGroup.Spec.MinMember, 1)
			}
			defaultPodSet.SetMinAvailable(minAvail)
			rootSubGroupSet.AddPodSet(defaultPodSet)
		}
	}
	pgi.invalidateTasksCache()
	return nil
}

func (pgi *PodGroupInfo) addTaskIndex(ti *pod_info.PodInfo) {
	if _, found := pgi.PodStatusIndex[ti.Status]; !found {
		pgi.PodStatusIndex[ti.Status] = pod_info.PodsMap{}
	}

	pgi.PodStatusIndex[ti.Status][ti.UID] = ti
	if pgi.allPodsMap != nil {
		(*pgi.allPodsMap)[ti.UID] = ti
	}
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
	podSet, found := pgi.PodSets[taskSubGroupName]
	if !found {
		log.InfraLogger.Warningf("AddTaskInfo for task <%s/%s> of podGroup: <%s/%s>: SubGroup not found <%s>", ti.Namespace, ti.Name, pgi.Namespace, pgi.Name, taskSubGroupName)
		return
	}

	podSet.AssignTask(ti)
	pgi.addTaskIndex(ti)

	if pod_status.AllocatedStatus(ti.Status) {
		pgi.AllocatedVector.Add(ti.ResReqVector)
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
		if pgi.allPodsMap != nil {
			delete(*pgi.allPodsMap, ti.UID)
		}
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
	pgi.allPodsMap = nil
	pgi.tasksToAllocate = nil
	pgi.tasksToAllocateInitResourceVector = nil
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
		pgi.AllocatedVector.Sub(task.ResReqVector)
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
			tasksTotalRequestedGPUs += task.ResReqVector.Get(resource_info.GPUIndex)
		}
	}

	return tasksTotalRequestedGPUs
}

func (pgi *PodGroupInfo) GetTasksActiveAllocatedReqResourceVector() resource_info.ResourceVector {
	result := resource_info.NewResourceVector(pgi.VectorMap)
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.IsActiveAllocatedStatus(task.Status) {
			result.Add(task.ResReqVector)
		}
	}
	return result
}

func (pgi *PodGroupInfo) IsReadyForScheduling() bool {
	if pgi.RootSubGroupSet == nil {
		return false
	}
	return pgi.RootSubGroupSet.IsReadyForScheduling()
}

func (pgi *PodGroupInfo) IsElastic() bool {
	for _, podSet := range pgi.PodSets {
		if podSet.IsElastic() {
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
	for _, podSet := range pgi.PodSets {
		if !podSet.IsGangSatisfied() {
			return true
		}
	}
	return false
}

func (pgi *PodGroupInfo) IsGangSatisfied() bool {
	for _, podSet := range pgi.PodSets {
		if !podSet.IsGangSatisfied() {
			return false
		}
	}
	return true
}

func (pgi *PodGroupInfo) ShouldPipelineJob() bool {
	for _, podSet := range pgi.PodSets {
		hasPipelinedTask := false
		activeAllocatedTasksCount := 0
		for _, task := range podSet.GetPodInfos() {
			if task.Status == pod_status.Pipelined {
				log.InfraLogger.V(7).Infof("task: <%v/%v> was pipelined to node: <%v>",
					task.Namespace, task.Name, task.NodeName)
				hasPipelinedTask = true
			} else if pod_status.IsActiveAllocatedStatus(task.Status) {
				activeAllocatedTasksCount += 1
			}
		}

		if hasPipelinedTask && activeAllocatedTasksCount < int(podSet.GetMinAvailable()) {
			log.InfraLogger.V(7).Infof("Subgroup: <%v/%v> has pipelined tasks, and not enough allocated pods for minAvailable <%v>. Pipeline all.",
				pgi.UID, podSet.GetName(), podSet.GetMinAvailable())
			return true
		}
	}
	return false
}

func (pgi *PodGroupInfo) Clone() *PodGroupInfo {
	return pgi.CloneWithTasks(maps.Values(pgi.GetAllPodsMap()))
}

// SetVectorMap sets the vector map and reinitializes AllocatedVector.
// Use this for deferred initialization when vectorMap is not available at construction time.
func (pgi *PodGroupInfo) SetVectorMap(vectorMap *resource_info.ResourceVectorMap) {
	pgi.VectorMap = vectorMap
	pgi.AllocatedVector = resource_info.NewResourceVector(vectorMap)
	for _, task := range pgi.GetAllPodsMap() {
		if pod_status.AllocatedStatus(task.Status) {
			pgi.AllocatedVector.Add(task.ResReqVector)
		}
	}
}

func (pgi *PodGroupInfo) CloneWithTasks(tasks []*pod_info.PodInfo) *PodGroupInfo {
	info := &PodGroupInfo{
		UID:            pgi.UID,
		Name:           pgi.Name,
		Namespace:      pgi.Namespace,
		Queue:          pgi.Queue,
		Priority:       pgi.Priority,
		Preemptibility: pgi.Preemptibility,

		AllocatedVector: resource_info.NewResourceVector(pgi.VectorMap),
		VectorMap:       pgi.VectorMap,

		JobFitErrors:   make([]common_info.JobFitError, 0),
		TasksFitErrors: make(map[common_info.PodID]*common_info.TasksFitErrors),

		PodGroup:    pgi.PodGroup,
		PodGroupUID: pgi.PodGroupUID,

		PodStatusIndex:       map[pod_status.PodStatus]pod_info.PodsMap{},
		activeAllocatedCount: ptr.To(0),
	}

	pgi.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	info.RootSubGroupSet = pgi.RootSubGroupSet.Clone()
	info.PodSets = info.RootSubGroupSet.GetDescendantPodSets()

	for _, task := range tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

func (pgi *PodGroupInfo) String() string {
	res := ""

	for _, podSet := range pgi.PodSets {
		res = res + fmt.Sprintf("\t\t subGroup %s: minAvailable(%v)\n",
			podSet.GetName(), podSet.GetMinAvailable())
	}

	i := 0
	for _, task := range pgi.GetAllPodsMap() {
		res = res + fmt.Sprintf("\n\t task %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): namespace %v (%v), name %v, podGroup %+v",
		pgi.UID, pgi.Namespace, pgi.Queue, pgi.Name, pgi.PodGroup) + res
}

func (pgi *PodGroupInfo) AddTaskFitErrors(task *pod_info.PodInfo, fitErrors *common_info.TasksFitErrors) {
	existingFitErrors, found := pgi.TasksFitErrors[task.UID]
	if found {
		existingFitErrors.AddNodeErrors(fitErrors)
	} else {
		pgi.TasksFitErrors[task.UID] = fitErrors
	}
}

func (pgi *PodGroupInfo) AddSimpleJobFitError(reason enginev2alpha2.UnschedulableReason, message string) {
	pgi.AddJobFitError(common_info.NewJobFitError(pgi.Name, DefaultSubGroup, pgi.Namespace, reason, []string{message}))
}

func (pgi *PodGroupInfo) AddJobFitError(err common_info.JobFitError) {
	pgi.JobFitErrors = append(pgi.JobFitErrors, err)
}

func (pgi *PodGroupInfo) GetSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if pgi.schedulingConstraintsSignature == "" {
		pgi.schedulingConstraintsSignature = pgi.generateSchedulingConstraintsSignature()
	}

	return pgi.schedulingConstraintsSignature
}

func (pgi *PodGroupInfo) generateSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	hash := sha256.New()
	var signatures []common_info.SchedulingConstraintsSignature

	for _, podSet := range pgi.PodSets {
		signatures = append(signatures, podSet.GetSchedulingConstraintsSignature())
	}

	slices.Sort(signatures)

	for _, signature := range signatures {
		hash.Write([]byte(signature))
	}

	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}
