// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_vectors

import (
	"sort"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const filterName = "ResourceVectorFeasibility"

type ResourceVectorFilter struct {
	nodeAvailable    map[string]resource_info.ResourceVector
	processedVictims map[common_info.PodID]bool
	pendingTasks     []*pod_info.PodInfo
}

func NewResourceVectorFilter(
	scenario *scenario.ByNodeScenario, nodeInfosMap map[string]*node_info.NodeInfo,
) *ResourceVectorFilter {
	if scenario == nil || len(scenario.PendingTasks()) == 0 {
		return nil
	}

	nodeAvailable := buildNodeAvailableMap(nodeInfosMap)
	processedVictims := make(map[common_info.PodID]bool)

	processNewVictims(scenario.RecordedVictimsTasks(), processedVictims, func(task *pod_info.PodInfo) {
		addFreedResources(nodeAvailable, task)
	})

	pendingTasks := sortPendingTasksByGPUDesc(scenario.PendingTasks())

	return &ResourceVectorFilter{
		nodeAvailable:    nodeAvailable,
		processedVictims: processedVictims,
		pendingTasks:     pendingTasks,
	}
}

func (f *ResourceVectorFilter) Name() string {
	return filterName
}

func (f *ResourceVectorFilter) Filter(scenario *scenario.ByNodeScenario) (bool, error) {
	processNewVictims(scenario.PotentialVictimsTasks(), f.processedVictims, func(task *pod_info.PodInfo) {
		addFreedResources(f.nodeAvailable, task)
	})

	return f.canFitAllTasks(), nil
}

func (f *ResourceVectorFilter) canFitAllTasks() bool {
	localAvailable := make(map[string]resource_info.ResourceVector, len(f.nodeAvailable))
	for name, vec := range f.nodeAvailable {
		localAvailable[name] = vec.Clone()
	}

	for _, task := range f.pendingTasks {
		if !placeTask(task.ResReqVector, localAvailable) {
			return false
		}
	}
	return true
}

func placeTask(taskVec resource_info.ResourceVector, available map[string]resource_info.ResourceVector) bool {
	for name, avail := range available {
		if taskVec.LessEqual(avail) {
			avail.Sub(taskVec)
			available[name] = avail
			return true
		}
	}
	return false
}

func buildNodeAvailableMap(nodeInfosMap map[string]*node_info.NodeInfo) map[string]resource_info.ResourceVector {
	available := make(map[string]resource_info.ResourceVector, len(nodeInfosMap))
	for name, ni := range nodeInfosMap {
		vec := ni.IdleVector.Clone()
		vec.Add(ni.ReleasingVector)
		available[name] = vec
	}
	return available
}

func addFreedResources(nodeAvailable map[string]resource_info.ResourceVector, task *pod_info.PodInfo) {
	freed := task.AcceptedResourceVector.Clone()
	if task.IsSharedGPUAllocation() {
		freed.Set(resource_info.GPUIndex, 0)
	}

	if existing, ok := nodeAvailable[task.NodeName]; ok {
		existing.Add(freed)
	}
}

func processNewVictims(
	victims []*pod_info.PodInfo,
	processedCache map[common_info.PodID]bool,
	fn func(*pod_info.PodInfo),
) {
	for _, task := range victims {
		if task.NodeName == "" || processedCache[task.UID] {
			continue
		}
		processedCache[task.UID] = true
		fn(task)
	}
}

func sortPendingTasksByGPUDesc(tasks []*pod_info.PodInfo) []*pod_info.PodInfo {
	sorted := make([]*pod_info.PodInfo, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ResReqVector.Get(resource_info.GPUIndex) >
			sorted[j].ResReqVector.Get(resource_info.GPUIndex)
	})
	return sorted
}
