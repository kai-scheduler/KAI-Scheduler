// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgroup_info

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

func TestNewPodSet(t *testing.T) {
	name := "my-subgroup"
	minAvailable := int32(4)
	podSet := NewPodSet(name, minAvailable, nil)

	if podSet.name != name {
		t.Errorf("Expected name %s, got %s", name, podSet.name)
	}
	if podSet.minAvailable != minAvailable {
		t.Errorf("Expected minAvailable %d, got %d", minAvailable, podSet.minAvailable)
	}
	if len(podSet.GetPodInfos()) != 0 {
		t.Errorf("Expected empty podInfos, got %d items", len(podSet.GetPodInfos()))
	}
}

func TestWithPodInfos(t *testing.T) {
	podSet := NewPodSet("test", 1, nil)

	// Pre-populate with a pod that should be cleared
	podSet.AssignTask(&pod_info.PodInfo{UID: "old", Status: pod_status.Running})

	// Prepare new pod infos
	p1 := &pod_info.PodInfo{UID: "pod1", Status: pod_status.Pending}
	p2 := &pod_info.PodInfo{UID: "pod2", Status: pod_status.Running}
	replacement := pod_info.PodsMap{
		"pod1": p1,
		"pod2": p2,
	}

	podSet.WithPodInfos(replacement)

	gotInfos := podSet.GetPodInfos()
	if len(gotInfos) != 2 {
		t.Errorf("expected len(podInfos)==2, got %d", len(gotInfos))
	}
	if gotInfos["pod1"] != p1 {
		t.Errorf("pod1 entry is not correct")
	}
	if gotInfos["pod2"] != p2 {
		t.Errorf("pod2 entry is not correct")
	}
	// Old pod should not be present
	if _, ok := gotInfos["old"]; ok {
		t.Errorf("expected old pod to be cleared by WithPodInfos")
	}

	// Check counters based on status
	if want := 2; podSet.GetNumAliveTasks() != want {
		t.Errorf("GetNumAliveTasks() = %d, want %d", podSet.GetNumAliveTasks(), want)
	}
	if want := 1; podSet.GetNumActiveAllocatedTasks() != want {
		t.Errorf("GetNumActiveAllocatedTasks() = %d, want %d", podSet.GetNumActiveAllocatedTasks(), want)
	}
	if want := 1; podSet.GetNumPendingTasks() != want {
		t.Errorf("GetNumPendingTasks() = %d, want %d", podSet.GetNumPendingTasks(), want)
	}
}

func TestGetMinAvailable(t *testing.T) {
	minAvailable := int32(3)
	podSet := NewPodSet("test", minAvailable, nil)

	if got := podSet.GetMinAvailable(); got != minAvailable {
		t.Errorf("GetMinAvailable() = %d, want %d", got, minAvailable)
	}
}

func TestSetMinAvailable(t *testing.T) {
	podSet := NewPodSet("test", 8, nil)
	newMinAvailable := int32(5)
	podSet.SetMinAvailable(newMinAvailable)

	if got := podSet.GetMinAvailable(); got != newMinAvailable {
		t.Errorf("After SetMinAvailable(%d), GetMinAvailable() = %d", newMinAvailable, got)
	}
}

func TestGetPodInfos(t *testing.T) {
	podSet := NewPodSet("test", 2, nil)

	// Should be empty initially
	if podInfos := podSet.GetPodInfos(); len(podInfos) != 0 {
		t.Errorf("Expected empty podInfos from GetPodInfos, got %d items", len(podInfos))
	}

	// Add a pod and check GetPodInfos
	pod := &pod_info.PodInfo{UID: "test-pod", Status: pod_status.Pending}
	podSet.AssignTask(pod)
	podInfos := podSet.GetPodInfos()
	if len(podInfos) != 1 {
		t.Errorf("Expected 1 pod in GetPodInfos, got %d", len(podInfos))
	}
	if podInfos["test-pod"] != pod {
		t.Error("GetPodInfos did not return expected pod mapping")
	}
}

func TestAddTaskInfoToPodSet(t *testing.T) {
	podSet := NewPodSet("test", 1, nil)
	podInfo := &pod_info.PodInfo{
		UID:    "pod-1",
		Status: pod_status.Pending,
	}

	podSet.AssignTask(podInfo)
	if len(podSet.podInfos) != 1 {
		t.Errorf("Expected 1 pod info object, got %d", len(podSet.podInfos))
	}
	if podSet.podInfos[podInfo.UID] != podInfo {
		t.Error("Pod info not properly stored in map")
	}

	podInfo2 := &pod_info.PodInfo{
		UID:    "pod-2",
		Status: pod_status.Pending,
	}
	podSet.AssignTask(podInfo2)
	if len(podSet.podInfos) != 2 {
		t.Errorf("Expected 2 pod info objects, got %d", len(podSet.podInfos))
	}

	if podSet.podInfos[podInfo2.UID] != podInfo2 {
		t.Error("Pod info 2 not properly stored in map")
	}
}

func TestIsReadyForScheduling(t *testing.T) {
	tests := []struct {
		name         string
		minAvailable int32
		pods         []*pod_info.PodInfo
		expected     bool
	}{
		{
			name:         "ready with exact minimum",
			minAvailable: 2,
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
				{UID: "2", Status: pod_status.Pending},
			},
			expected: true,
		},
		{
			name:         "ready with more than minimum",
			minAvailable: 1,
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
				{UID: "2", Status: pod_status.Pending},
			},
			expected: true,
		},
		{
			name:         "not ready with gated pods",
			minAvailable: 2,
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
				{UID: "2", Status: pod_status.Gated},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podSet := NewPodSet("test", tt.minAvailable, nil)
			for _, pod := range tt.pods {
				podSet.AssignTask(pod)
			}
			if got := podSet.IsReadyForScheduling(); got != tt.expected {
				t.Errorf("IsReadyForScheduling() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsGangSatisfied(t *testing.T) {
	tests := []struct {
		name         string
		minAvailable int32
		pods         []*pod_info.PodInfo
		expected     bool
	}{
		{
			name:         "satisfied with exact minimum",
			minAvailable: 2,
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Running},
				{UID: "2", Status: pod_status.Running},
			},
			expected: true,
		},
		{
			name:         "not satisfied with insufficient active pods",
			minAvailable: 2,
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Running},
				{UID: "2", Status: pod_status.Failed},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podSet := NewPodSet("test", tt.minAvailable, nil)
			for _, pod := range tt.pods {
				podSet.AssignTask(pod)
			}
			if got := podSet.IsGangSatisfied(); got != tt.expected {
				t.Errorf("IsGangSatisfied() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetNumAliveTasks(t *testing.T) {
	podSet := NewPodSet("test", 2, nil)
	pods := []*pod_info.PodInfo{
		{UID: "1", Status: pod_status.Running},
		{UID: "2", Status: pod_status.Pending},
		{UID: "3", Status: pod_status.Failed},
		{UID: "4", Status: pod_status.Succeeded},
	}

	for _, pod := range pods {
		podSet.AssignTask(pod)
	}

	expected := 2 // Running and Pending are alive statuses
	if got := podSet.GetNumAliveTasks(); got != expected {
		t.Errorf("GetNumAliveTasks() = %v, want %v", got, expected)
	}
}

func TestGetNumActiveUsedTasks(t *testing.T) {
	podSet := NewPodSet("test", 2, nil)
	pods := []*pod_info.PodInfo{
		{UID: "1", Status: pod_status.Running},
		{UID: "2", Status: pod_status.Pipelined},
		{UID: "3", Status: pod_status.Releasing},
		{UID: "4", Status: pod_status.Failed},
		{UID: "5", Status: pod_status.Succeeded},
	}

	for _, pod := range pods {
		podSet.AssignTask(pod)
	}

	expected := 3 // Running, Pipelined, and Releasing are considered active used statuses
	if got := podSet.GetNumActiveUsedTasks(); got != expected {
		t.Errorf("GetNumActiveUsedTasks() = %v, want %v", got, expected)
	}
}

func TestGetNumGatedTasks(t *testing.T) {
	podSet := NewPodSet("test", 2, nil)
	pods := []*pod_info.PodInfo{
		{UID: "1", Status: pod_status.Gated},
		{UID: "2", Status: pod_status.Running},
		{UID: "3", Status: pod_status.Gated},
		{UID: "4", Status: pod_status.Pending},
	}

	for _, pod := range pods {
		podSet.AssignTask(pod)
	}

	expected := 2 // Two Gated pods
	if got := podSet.GetNumGatedTasks(); got != expected {
		t.Errorf("GetNumGatedTasks() = %v, want %v", got, expected)
	}
}

func TestGetNumPendingTasks(t *testing.T) {
	podSet := NewPodSet("test", 2, nil)
	pods := []*pod_info.PodInfo{
		{UID: "1", Status: pod_status.Pending},
		{UID: "2", Status: pod_status.Running},
		{UID: "3", Status: pod_status.Pending},
		{UID: "4", Status: pod_status.Gated},
		{UID: "5", Status: pod_status.Pending},
	}

	for _, pod := range pods {
		podSet.AssignTask(pod)
	}

	expected := 3 // Three Pending pods
	if got := podSet.GetNumPendingTasks(); got != expected {
		t.Errorf("GetNumPendingTasks() = %v, want %v", got, expected)
	}
}

func TestIsElastic(t *testing.T) {
	tests := []struct {
		name     string
		pods     []*pod_info.PodInfo
		expected bool
	}{
		{
			name: "satisfied with exact minimum",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Running},
				{UID: "2", Status: pod_status.Running},
			},
			expected: false,
		},
		{
			name: "not satisfied with insufficient pods",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
			},
			expected: false,
		},
		{
			name: "satisfied with above minimum pods",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
				{UID: "2", Status: pod_status.Running},
				{UID: "3", Status: pod_status.Pending},
				{UID: "4", Status: pod_status.Gated},
				{UID: "5", Status: pod_status.Pending},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		podSet := NewPodSet("test", 2, nil)
		for _, pod := range test.pods {
			podSet.AssignTask(pod)
		}

		if got := podSet.IsElastic(); got != test.expected {
			t.Errorf("Name: %v, IsElastic() = %v, want %v", test.name, got, test.expected)
		}
	}
}

func TestGetNumActiveAllocatedTasks(t *testing.T) {
	tests := []struct {
		name     string
		pods     []*pod_info.PodInfo
		expected int
	}{
		{
			name:     "no pods",
			pods:     nil,
			expected: 0,
		},
		{
			name: "all pods not active/allocated",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Pending},
				{UID: "2", Status: pod_status.Gated},
			},
			expected: 0,
		},
		{
			name: "one active allocated",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Allocated},
				{UID: "2", Status: pod_status.Pending},
			},
			expected: 1,
		},
		{
			name: "multiple active allocated",
			pods: []*pod_info.PodInfo{
				{UID: "1", Status: pod_status.Allocated},
				{UID: "2", Status: pod_status.Running},
				{UID: "3", Status: pod_status.Gated},
				{UID: "4", Status: pod_status.Allocated},
			},
			expected: 3, // Allocated and Running are assumed active allocated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podSet := NewPodSet("test", 1, nil)
			for _, pod := range tt.pods {
				podSet.AssignTask(pod)
			}
			got := podSet.GetNumActiveAllocatedTasks()
			if got != tt.expected {
				t.Errorf("GetNumActiveAllocatedTasks() = %d, want %d", got, tt.expected)
			}
		})
	}
}
