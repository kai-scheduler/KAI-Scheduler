// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

type SubGroupInfo struct {
	name                    string
	minAvailable            int32
	podInfos                pod_info.PodsMap
	podStatusIndex          map[pod_status.PodStatus]pod_info.PodsMap
	podStatusMap            map[common_info.PodID]pod_status.PodStatus
	numActiveAllocatedTasks int
	numActiveUsedTasks      int
	numAliveTasks           int
}

func NewSubGroupInfo(name string, minAvailable int32) *SubGroupInfo {
	return &SubGroupInfo{
		name:                    name,
		minAvailable:            minAvailable,
		podInfos:                pod_info.PodsMap{},
		podStatusIndex:          map[pod_status.PodStatus]pod_info.PodsMap{},
		podStatusMap:            map[common_info.PodID]pod_status.PodStatus{},
		numActiveAllocatedTasks: 0,
		numActiveUsedTasks:      0,
		numAliveTasks:           0,
	}
}

func FromSubGroup(subGroup *v2alpha2.SubGroup) *SubGroupInfo {
	return NewSubGroupInfo(subGroup.Name, subGroup.MinMember)
}

func (sgi *SubGroupInfo) GetName() string {
	return sgi.name
}

func (sgi *SubGroupInfo) GetMinAvailable() int32 {
	return sgi.minAvailable
}

func (sgi *SubGroupInfo) SetMinAvailable(value int32) {
	sgi.minAvailable = value
}

func (sgi *SubGroupInfo) GetPodInfos() pod_info.PodsMap {
	return sgi.podInfos
}

func (sgi *SubGroupInfo) AssignTask(ti *pod_info.PodInfo) {
	sgi.clearOldStatus(ti)

	if _, found := sgi.podStatusIndex[ti.Status]; !found {
		sgi.podStatusIndex[ti.Status] = pod_info.PodsMap{}
	}
	sgi.podStatusIndex[ti.Status][ti.UID] = ti

	if pod_status.IsActiveAllocatedStatus(ti.Status) {
		sgi.numActiveAllocatedTasks += 1
	}
	if pod_status.IsActiveUsedStatus(ti.Status) {
		sgi.numActiveUsedTasks += 1
	}
	if pod_status.IsAliveStatus(ti.Status) {
		sgi.numAliveTasks += 1
	}

	sgi.podStatusMap[ti.UID] = ti.Status
	sgi.podInfos[ti.UID] = ti
}

func (sgi *SubGroupInfo) clearOldStatus(ti *pod_info.PodInfo) {
	oldStatus, found := sgi.podStatusMap[ti.UID]
	if !found {
		return
	}
	if pod_status.IsActiveAllocatedStatus(oldStatus) {
		sgi.numActiveAllocatedTasks -= 1
	}
	if pod_status.IsActiveUsedStatus(oldStatus) {
		sgi.numActiveUsedTasks -= 1
	}
	if pod_status.IsAliveStatus(oldStatus) {
		sgi.numAliveTasks -= 1
	}

	delete(sgi.podStatusIndex[oldStatus], ti.UID)
	delete(sgi.podStatusMap, ti.UID)
	delete(sgi.podInfos, ti.UID)
}

func (sgi *SubGroupInfo) WithPodInfos(tasks pod_info.PodsMap) *SubGroupInfo {
	for _, oldTask := range sgi.podInfos {
		sgi.clearOldStatus(oldTask)
	}

	for _, task := range tasks {
		sgi.AssignTask(task)
	}
	return sgi
}

func (sgi *SubGroupInfo) IsReadyForScheduling() bool {
	readyTasks := sgi.GetNumAliveTasks() - sgi.GetNumGatedTasks()
	return int32(readyTasks) >= sgi.minAvailable
}

func (sgi *SubGroupInfo) IsGangSatisfied() bool {
	numActiveTasks := sgi.GetNumActiveUsedTasks()
	return numActiveTasks >= int(sgi.minAvailable)
}

func (sgi *SubGroupInfo) IsElastic() bool {
	return sgi.GetMinAvailable() < int32(len(sgi.GetPodInfos()))
}

func (sgi *SubGroupInfo) GetNumActiveAllocatedTasks() int {
	return sgi.numActiveAllocatedTasks
}

func (sgi *SubGroupInfo) GetNumActiveUsedTasks() int {
	return sgi.numActiveUsedTasks
}

func (sgi *SubGroupInfo) GetNumAliveTasks() int {
	return sgi.numAliveTasks
}

func (sgi *SubGroupInfo) GetNumGatedTasks() int {
	return len(sgi.podStatusIndex[pod_status.Gated])
}

func (sgi *SubGroupInfo) GetNumPendingTasks() int {
	return len(sgi.podStatusIndex[pod_status.Pending])
}
