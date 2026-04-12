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
	"k8s.io/utils/ptr"
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

func TestPodSetOrderFn(t *testing.T) {
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

type podSetSpec struct {
	minAvail int32
	numAlloc int
}

func makeSubGroupSetWithPodSetChildren(name string, minSubGroup *int32, childSpecs []podSetSpec) *subgroup_info.SubGroupSet {
	sgs := subgroup_info.NewSubGroupSet(name, nil)
	sgs.SetMinSubGroup(minSubGroup)
	for i, spec := range childSpecs {
		childName := fmt.Sprintf("%s-child-%d", name, i)
		ps := makeSubGroupInfoWithAllocated(spec.minAvail, spec.numAlloc, childName)
		sgs.AddPodSet(ps)
	}
	return sgs
}

func TestSubGroupSetOrderFn(t *testing.T) {
	tests := []struct {
		name  string
		left  *subgroup_info.SubGroupSet
		right *subgroup_info.SubGroupSet
		want  int
	}{
		{
			name: "both below threshold (nil minSubGroup, no children satisfied)",
			left: makeSubGroupSetWithPodSetChildren("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 1},
				{minAvail: 3, numAlloc: 0},
			}),
			right: makeSubGroupSetWithPodSetChildren("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 2},
				{minAvail: 3, numAlloc: 1},
			}),
			want: equalPrioritization,
		},
		{
			name: "left below threshold, right above",
			left: makeSubGroupSetWithPodSetChildren("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 0},
				{minAvail: 3, numAlloc: 0},
			}),
			right: makeSubGroupSetWithPodSetChildren("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			want: lPrioritized,
		},
		{
			name: "right below threshold, left above",
			left: makeSubGroupSetWithPodSetChildren("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			right: makeSubGroupSetWithPodSetChildren("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 0},
				{minAvail: 3, numAlloc: 0},
			}),
			want: rPrioritized,
		},
		{
			name: "both satisfied, left has lower satisfaction ratio",
			// left: minSubGroup=2, 2 of 3 children satisfied → ratio=1.0
			// right: minSubGroup=2, 3 of 3 children satisfied → ratio=1.5
			left: func() *subgroup_info.SubGroupSet {
				sgs := subgroup_info.NewSubGroupSet("l", nil)
				sgs.SetMinSubGroup(ptr.To(int32(2)))
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 5, "l-0")) // satisfied
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 4, "l-1")) // satisfied
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 0, "l-2")) // not satisfied
				return sgs
			}(),
			right: func() *subgroup_info.SubGroupSet {
				sgs := subgroup_info.NewSubGroupSet("r", nil)
				sgs.SetMinSubGroup(ptr.To(int32(2)))
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 5, "r-0")) // satisfied
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 4, "r-1")) // satisfied
				sgs.AddPodSet(makeSubGroupInfoWithAllocated(3, 3, "r-2")) // satisfied
				return sgs
			}(),
			want: lPrioritized,
		},
		{
			name: "both satisfied, equal satisfaction ratio",
			// left: minSubGroup=2, 4 satisfied → ratio=2.0
			// right: minSubGroup=4, 8 satisfied → ratio=2.0
			left: func() *subgroup_info.SubGroupSet {
				sgs := subgroup_info.NewSubGroupSet("l", nil)
				sgs.SetMinSubGroup(ptr.To(int32(2)))
				for i := 0; i < 4; i++ {
					sgs.AddPodSet(makeSubGroupInfoWithAllocated(1, 1, fmt.Sprintf("l-%d", i)))
				}
				return sgs
			}(),
			right: func() *subgroup_info.SubGroupSet {
				sgs := subgroup_info.NewSubGroupSet("r", nil)
				sgs.SetMinSubGroup(ptr.To(int32(4)))
				for i := 0; i < 8; i++ {
					sgs.AddPodSet(makeSubGroupInfoWithAllocated(1, 1, fmt.Sprintf("r-%d", i)))
				}
				return sgs
			}(),
			want: equalPrioritization,
		},
		{
			name: "left optional (minSubGroup=0), right required and satisfied",
			left: makeSubGroupSetWithPodSetChildren("l", ptr.To(int32(0)), []podSetSpec{}),
			right: makeSubGroupSetWithPodSetChildren("r", ptr.To(int32(2)), []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			want: rPrioritized,
		},
		{
			name: "nested SubGroupSets: left's child not satisfied, right's child satisfied",
			left: func() *subgroup_info.SubGroupSet {
				// outer left: minSubGroup=1, one child SubGroupSet not satisfied
				// child: two PodSets, only one satisfied
				child := makeSubGroupSetWithPodSetChildren("l-child", nil, []podSetSpec{
					{minAvail: 2, numAlloc: 3},
					{minAvail: 2, numAlloc: 0},
				})
				sgs := subgroup_info.NewSubGroupSet("l", nil)
				sgs.SetMinSubGroup(ptr.To(int32(1)))
				sgs.AddSubGroup(child)
				return sgs
			}(),
			right: func() *subgroup_info.SubGroupSet {
				// outer right: minSubGroup=1, one child SubGroupSet satisfied
				// child: two PodSets both satisfied
				child := makeSubGroupSetWithPodSetChildren("r-child", nil, []podSetSpec{
					{minAvail: 2, numAlloc: 3},
					{minAvail: 2, numAlloc: 4},
				})
				sgs := subgroup_info.NewSubGroupSet("r", nil)
				sgs.SetMinSubGroup(ptr.To(int32(1)))
				sgs.AddSubGroup(child)
				return sgs
			}(),
			want: lPrioritized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubGroupSetOrderFn(tt.left, tt.right)
			if got != tt.want {
				t.Errorf("SubGroupSetOrderFn() = %v, want %v", got, tt.want)
			}
		})
	}
}
