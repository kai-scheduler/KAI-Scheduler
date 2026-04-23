// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

type PodSet struct {
	SubGroupInfo

	minAvailable            int32
	podInfos                pod_info.PodsMap
	podStatusIndex          map[pod_status.PodStatus]pod_info.PodsMap
	podStatusMap            map[common_info.PodID]pod_status.PodStatus
	numActiveAllocatedTasks int
	numActiveUsedTasks      int
	numAliveTasks           int

	schedulingConstraintsSignature common_info.SchedulingConstraintsSignature
}

func NewPodSet(name string, minAvailable int32, topologyConstraint *topology_info.TopologyConstraintInfo) *PodSet {
	return &PodSet{
		SubGroupInfo:            *newSubGroupInfo(name, topologyConstraint),
		minAvailable:            minAvailable,
		podInfos:                pod_info.PodsMap{},
		podStatusIndex:          map[pod_status.PodStatus]pod_info.PodsMap{},
		podStatusMap:            map[common_info.PodID]pod_status.PodStatus{},
		numActiveAllocatedTasks: 0,
		numActiveUsedTasks:      0,
		numAliveTasks:           0,
	}
}

func (ps *PodSet) GetMinAvailable() int32 {
	return ps.minAvailable
}

func (ps *PodSet) SetMinAvailable(value int32) {
	ps.minAvailable = value
}

func (ps *PodSet) GetPodInfos() pod_info.PodsMap {
	return ps.podInfos
}

func (ps *PodSet) AssignTask(ti *pod_info.PodInfo) {
	ps.invalidateSchedulingConstraintsSignature()
	ps.clearOldStatus(ti)

	if _, found := ps.podStatusIndex[ti.Status]; !found {
		ps.podStatusIndex[ti.Status] = pod_info.PodsMap{}
	}
	ps.podStatusIndex[ti.Status][ti.UID] = ti

	if pod_status.IsActiveAllocatedStatus(ti.Status) {
		ps.numActiveAllocatedTasks += 1
	}
	if pod_status.IsActiveUsedStatus(ti.Status) {
		ps.numActiveUsedTasks += 1
	}
	if pod_status.IsAliveStatus(ti.Status) {
		ps.numAliveTasks += 1
	}

	ps.podStatusMap[ti.UID] = ti.Status
	ps.podInfos[ti.UID] = ti
}

func (ps *PodSet) invalidateSchedulingConstraintsSignature() {
	ps.schedulingConstraintsSignature = ""
}

func (ps *PodSet) clearOldStatus(ti *pod_info.PodInfo) {
	oldStatus, found := ps.podStatusMap[ti.UID]
	if !found {
		return
	}
	if pod_status.IsActiveAllocatedStatus(oldStatus) {
		ps.numActiveAllocatedTasks -= 1
	}
	if pod_status.IsActiveUsedStatus(oldStatus) {
		ps.numActiveUsedTasks -= 1
	}
	if pod_status.IsAliveStatus(oldStatus) {
		ps.numAliveTasks -= 1
	}

	delete(ps.podStatusIndex[oldStatus], ti.UID)
	delete(ps.podStatusMap, ti.UID)
	delete(ps.podInfos, ti.UID)
}

func (ps *PodSet) WithPodInfos(tasks pod_info.PodsMap) *PodSet {
	for _, oldTask := range ps.podInfos {
		ps.clearOldStatus(oldTask)
	}

	for _, task := range tasks {
		ps.AssignTask(task)
	}
	return ps
}

func (ps *PodSet) IsReadyForScheduling() bool {
	readyTasks := ps.GetNumAliveTasks() - ps.GetNumGatedTasks()
	if int32(readyTasks) < ps.minAvailable {
		return false
	}
	return true
}

func (ps *PodSet) IsGangSatisfied() bool {
	numActiveTasks := ps.GetNumActiveUsedTasks()
	return numActiveTasks >= int(ps.minAvailable)
}

func (ps *PodSet) IsElastic() bool {
	return ps.GetMinAvailable() < int32(len(ps.GetPodInfos()))
}

func (ps *PodSet) GetNumActiveAllocatedTasks() int {
	return ps.numActiveAllocatedTasks
}

func (ps *PodSet) GetNumActiveUsedTasks() int {
	return ps.numActiveUsedTasks
}

func (ps *PodSet) GetNumAliveTasks() int {
	return ps.numAliveTasks
}

func (ps *PodSet) GetNumGatedTasks() int {
	return len(ps.podStatusIndex[pod_status.Gated])
}

func (ps *PodSet) GetNumPendingTasks() int {
	return len(ps.podStatusIndex[pod_status.Pending])
}

func (ps *PodSet) Clone() *PodSet {
	return NewPodSet(ps.GetName(), ps.GetMinAvailable(), ps.GetTopologyConstraint())
}

func (ps *PodSet) GetSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	if ps.isAllPodsActiveAllocated() {
		// No scheduling constraints
		return ""
	}

	if ps.schedulingConstraintsSignature == "" {
		ps.schedulingConstraintsSignature = ps.generateSchedulingConstraintsSignature()
	}

	return ps.schedulingConstraintsSignature
}

func (ps *PodSet) GetNumActiveAllocatedMembers() int {
	return ps.GetNumActiveAllocatedTasks()
}

func (ps *PodSet) GetMinMembersToSatisfy() int {
	return int(ps.GetMinAvailable())
}

func (ps *PodSet) generateSchedulingConstraintsSignature() common_info.SchedulingConstraintsSignature {
	hash := sha256.New()

	// Topology Constraints
	// Use separator between levels so that different hierarchy orderings produce different hashes.
	// e.g., root="" + podset="x" should differ from root="x" + podset=""
	hash.Write([]byte(ps.GetTopologyConstraint().GetSchedulingConstraintsSignature()))
	for parent := ps.GetParent(); parent != nil; parent = parent.GetParent() {
		hash.Write([]byte("|"))
		hash.Write([]byte(parent.GetTopologyConstraint().GetSchedulingConstraintsSignature()))
	}

	// Pods
	podSignatures := make([]common_info.SchedulingConstraintsSignature, 0, len(ps.GetPodInfos()))
	for _, pod := range ps.GetPodInfos() {
		if pod_status.IsActiveAllocatedStatus(pod.Status) {
			continue
		}

		podSignatures = append(podSignatures, pod.GetSchedulingConstraintsSignature())
	}

	slices.Sort(podSignatures)
	for _, signature := range podSignatures {
		hash.Write([]byte(signature))
	}

	return common_info.SchedulingConstraintsSignature(fmt.Sprintf("%x", hash.Sum(nil)))
}

func (ps *PodSet) isAllPodsActiveAllocated() bool {
	return ps.GetNumActiveAllocatedTasks() == len(ps.GetPodInfos())
}
