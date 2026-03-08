// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package subgrouporder

import (
	"fmt"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

func makeAllocatedPodInfo(subGroupName string, taskIndex int) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		SubGroupName: subGroupName,
		Status:       pod_status.Running,
		UID:          common_info.PodID(fmt.Sprintf("%s-pod-%d", subGroupName, taskIndex)),
	}
}

func makeSubGroupInfoWithAllocated(minAvailable int32, numAllocated int, name string) *subgroup_info.PodSet {
	sg := subgroup_info.NewPodSet(name, minAvailable, nil)
	for i := 0; i < numAllocated; i++ {
		pod := makeAllocatedPodInfo(name, i)
		sg.AssignTask(pod)
	}
	return sg
}

func TestSubGroupOrderFn(t *testing.T) {
	tests := []struct {
		name          string
		lMinAvailable int32
		lAllocated    int
		rMinAvailable int32
		rAllocated    int
		want          int
	}{
		{
			name:          "both below minAvailable, should be equal",
			lMinAvailable: 3,
			lAllocated:    1,
			rMinAvailable: 4,
			rAllocated:    2,
			want:          equalPrioritization,
		},
		{
			name:          "left below, right above minAvailable",
			lMinAvailable: 3,
			lAllocated:    1,
			rMinAvailable: 3,
			rAllocated:    5,
			want:          lPrioritized,
		},
		{
			name:          "right below, left above minAvailable",
			lMinAvailable: 3,
			lAllocated:    5,
			rMinAvailable: 3,
			rAllocated:    1,
			want:          rPrioritized,
		},
		{
			name:          "both above minAvailable, left lower allocation ratio",
			lMinAvailable: 2,
			lAllocated:    4,
			rMinAvailable: 4,
			rAllocated:    9,
			want:          lPrioritized,
		},
		{
			name:          "both above minAvailable, right lower allocation ratio",
			lMinAvailable: 2,
			lAllocated:    10,
			rMinAvailable: 4,
			rAllocated:    9,
			want:          rPrioritized,
		},
		{
			name:          "both above minAvailable, equal allocation ratio",
			lMinAvailable: 2,
			lAllocated:    4,
			rMinAvailable: 4,
			rAllocated:    8,
			want:          equalPrioritization,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := makeSubGroupInfoWithAllocated(tt.lMinAvailable, tt.lAllocated, "l")
			right := makeSubGroupInfoWithAllocated(tt.rMinAvailable, tt.rAllocated, "r")
			got := PodSetOrderFn(left, right)
			if got != tt.want {
				t.Errorf("PodSetOrderFn() = %v, want %v", got, tt.want)
			}
		})
	}
}
