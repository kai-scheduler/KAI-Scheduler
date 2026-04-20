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

type podSetSpec struct {
	minAvail int32
	numAlloc int
}

func TestSubGroupOrderFn(t *testing.T) {
	tests := []struct {
		name  string
		left  subgroup_info.SubGroupMember
		right subgroup_info.SubGroupMember
		want  int
	}{
		{
			name: "both below threshold (nil minSubGroup, no members satisfied)",
			left: makeSubGroupSetWithPodSetMembers("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 1},
				{minAvail: 3, numAlloc: 0},
			}),
			right: makeSubGroupSetWithPodSetMembers("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 2},
				{minAvail: 3, numAlloc: 1},
			}),
			want: equalPrioritization,
		},
		{
			name: "left below threshold, right above",
			left: makeSubGroupSetWithPodSetMembers("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 0},
				{minAvail: 3, numAlloc: 0},
			}),
			right: makeSubGroupSetWithPodSetMembers("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			want: lPrioritized,
		},
		{
			name: "right below threshold, left above",
			left: makeSubGroupSetWithPodSetMembers("l", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			right: makeSubGroupSetWithPodSetMembers("r", nil, []podSetSpec{
				{minAvail: 3, numAlloc: 0},
				{minAvail: 3, numAlloc: 0},
			}),
			want: rPrioritized,
		},
		{
			name: "both satisfied, left has lower satisfaction ratio",
			// left: minSubGroup=2, 2 of 3 members satisfied → ratio=1.0
			// right: minSubGroup=2, 3 of 3 members satisfied → ratio=1.5
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
			// left: minSubGroup=2, 4 members satisfied → ratio=2.0
			// right: minSubGroup=4, 8 members satisfied → ratio=2.0
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
			left: makeSubGroupSetWithPodSetMembers("l", ptr.To(int32(0)), []podSetSpec{}),
			right: makeSubGroupSetWithPodSetMembers("r", ptr.To(int32(2)), []podSetSpec{
				{minAvail: 3, numAlloc: 5},
				{minAvail: 3, numAlloc: 4},
			}),
			want: rPrioritized,
		},
		{
			name: "nested SubGroupSets: left's member not satisfied, right's member satisfied",
			left: func() *subgroup_info.SubGroupSet {
				// outer left: minSubGroup=1, one member SubGroupSet not satisfied
				// member: two PodSets, only one satisfied
				member := makeSubGroupSetWithPodSetMembers("l-member", nil, []podSetSpec{
					{minAvail: 2, numAlloc: 3},
					{minAvail: 2, numAlloc: 0},
				})
				sgs := subgroup_info.NewSubGroupSet("l", nil)
				sgs.SetMinSubGroup(ptr.To(int32(1)))
				sgs.AddSubGroup(member)
				return sgs
			}(),
			right: func() *subgroup_info.SubGroupSet {
				// outer right: minSubGroup=1, one member SubGroupSet satisfied
				// member: two PodSets both satisfied
				member := makeSubGroupSetWithPodSetMembers("r-member", nil, []podSetSpec{
					{minAvail: 2, numAlloc: 3},
					{minAvail: 2, numAlloc: 4},
				})
				sgs := subgroup_info.NewSubGroupSet("r", nil)
				sgs.SetMinSubGroup(ptr.To(int32(1)))
				sgs.AddSubGroup(member)
				return sgs
			}(),
			want: lPrioritized,
		},
		{
			name:  "both below minAvailable, should be equal (PodSet)",
			left:  makeSubGroupInfoWithAllocated(3, 1, "l"),
			right: makeSubGroupInfoWithAllocated(4, 2, "r"),
			want:  equalPrioritization,
		},
		{
			name:  "left below, right above minAvailable (PodSet)",
			left:  makeSubGroupInfoWithAllocated(3, 1, "l"),
			right: makeSubGroupInfoWithAllocated(3, 5, "r"),
			want:  lPrioritized,
		},
		{
			name:  "right below, left above minAvailable (PodSet)",
			left:  makeSubGroupInfoWithAllocated(3, 5, "l"),
			right: makeSubGroupInfoWithAllocated(3, 1, "r"),
			want:  rPrioritized,
		},
		{
			name:  "both above minAvailable, left lower allocation ratio (PodSet)",
			left:  makeSubGroupInfoWithAllocated(2, 4, "l"),
			right: makeSubGroupInfoWithAllocated(4, 9, "r"),
			want:  lPrioritized,
		},
		{
			name:  "both above minAvailable, right lower allocation ratio (PodSet)",
			left:  makeSubGroupInfoWithAllocated(2, 10, "l"),
			right: makeSubGroupInfoWithAllocated(4, 9, "r"),
			want:  rPrioritized,
		},
		{
			name:  "both above minAvailable, equal allocation ratio (PodSet)",
			left:  makeSubGroupInfoWithAllocated(2, 4, "l"),
			right: makeSubGroupInfoWithAllocated(4, 8, "r"),
			want:  equalPrioritization,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubGroupOrderFn(tt.left, tt.right)
			if got != tt.want {
				t.Errorf("SubGroupOrderFn() = %v, want %v", got, tt.want)
			}
		})
	}
}

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

func makeSubGroupSetWithPodSetMembers(name string, minSubGroup *int32, memberSpecs []podSetSpec) *subgroup_info.SubGroupSet {
	sgs := subgroup_info.NewSubGroupSet(name, nil)
	sgs.SetMinSubGroup(minSubGroup)
	for i, spec := range memberSpecs {
		memberName := fmt.Sprintf("%s-member-%d", name, i)
		ps := makeSubGroupInfoWithAllocated(spec.minAvail, spec.numAlloc, memberName)
		sgs.AddPodSet(ps)
	}
	return sgs
}
