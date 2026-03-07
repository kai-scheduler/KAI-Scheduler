// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"sort"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

type MinimalJobRepresentatives struct {
	representatives map[common_info.SchedulingConstraintsSignature]*podgroup_info.PodGroupInfo
}

func NewMinimalJobRepresentatives() *MinimalJobRepresentatives {
	return &MinimalJobRepresentatives{
		representatives: make(map[common_info.SchedulingConstraintsSignature]*podgroup_info.PodGroupInfo),
	}
}

func (m *MinimalJobRepresentatives) IsEasierToSchedule(otherJob *podgroup_info.PodGroupInfo) (bool, *podgroup_info.PodGroupInfo) {
	key := otherJob.GetSchedulingConstraintsSignature()

	representative, found := m.representatives[key]
	if !found {
		return true, nil
	}

	return jobEasierToScheduleComparison(otherJob, representative), representative
}

func (m *MinimalJobRepresentatives) UpdateRepresentative(newJob *podgroup_info.PodGroupInfo) {
	key := newJob.GetSchedulingConstraintsSignature()
	currentRepresentative, found := m.representatives[key]

	if found && !isPodGroupFootprintSmaller(newJob, currentRepresentative) {
		return
	}

	m.representatives[key] = newJob
}

func jobEasierToScheduleComparison(podGroup1, podGroup2 *podgroup_info.PodGroupInfo) bool {
	pendingTasks1 := podGroup1.GetPendingTasks()
	pendingTasks2 := podGroup2.GetPendingTasks()

	if len(pendingTasks1) == 0 || len(pendingTasks2) == 0 {
		return false
	}
	if len(pendingTasks2) > len(pendingTasks1) {
		// If pg2 has more pods than pg1, it might make it harder to schedule.
		return true
	}

	pg1TasksResources := extractSortedResourceRequests(pendingTasks1)
	pg2TasksResources := extractSortedResourceRequests(pendingTasks2)

	for index := range pg1TasksResources {
		if index >= len(pg2TasksResources) {
			// If we reached until this point, pg1 is harder to schedule then pg2
			// for every pod in pg2, pg1 has at least one pod that is "not easier to schedule"
			return false
		}
		if pg1TasksResources[index].LessEqual(pg2TasksResources[index]) {
			if pg2TasksResources[index].LessEqual(pg1TasksResources[index]) {
				continue // They are equal
			}
			return true
		}
	}

	return false
}

// isPodGroupFootprintSmaller checks that for every pod from podGroup1 there is a larger/equal pod from podGroup2
func isPodGroupFootprintSmaller(podGroup1, podGroup2 *podgroup_info.PodGroupInfo) bool {
	pendingTasks1 := podGroup1.GetPendingTasks()
	pendingTasks2 := podGroup2.GetPendingTasks()

	if len(pendingTasks1) == 0 || len(pendingTasks2) == 0 {
		return false
	}
	if len(pendingTasks1) > len(pendingTasks2) {
		return false
	}

	pg1TasksResources := extractSortedResourceRequests(pendingTasks1)
	pg2TasksResources := extractSortedResourceRequests(pendingTasks2)

	for index := range pg1TasksResources {
		if !pg1TasksResources[index].LessEqual(pg2TasksResources[index]) {
			return false
		}
	}

	return true
}

func extractSortedResourceRequests(tasks []*pod_info.PodInfo) []*resource_info.ResourceRequirements {
	tasksResources := make([]*resource_info.ResourceRequirements, len(tasks))
	for i, task := range tasks {
		tasksResources[i] = task.ResReq
	}
	sort.Slice(tasksResources, func(i, j int) bool {
		return tasksResources[i].LessEqual(tasksResources[j])
	})
	return tasksResources
}
