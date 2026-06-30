// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

// segmentedJob builds a root SubGroupSet with minSubGroup=k over n leaf PodSets ("segment-i"),
// each with minAvailable=segmentSize and allocatedPerSegment[i] running pods.
func segmentedJob(k *int32, n, segmentSize int, allocatedPerSegment []int) *PodGroupInfo {
	root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	root.SetMinSubGroup(k)
	for i := 0; i < n; i++ {
		segName := fmt.Sprintf("segment-%d", i)
		ps := subgroup_info.NewPodSet(segName, int32(segmentSize), nil)
		for p := 0; p < allocatedPerSegment[i]; p++ {
			ps.AssignTask(simpleTask(fmt.Sprintf("%s-p%d", segName, p), segName, pod_status.Running))
		}
		root.AddPodSet(ps)
	}
	return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
}

func TestGetCoreTasks(t *testing.T) {
	tests := []struct {
		name              string
		job               *PodGroupInfo
		expectedCoreCount int
		expectedCoreNames []string
	}{
		{
			name: "FlatPodSet_CoreIsMinMember",
			job: func() *PodGroupInfo {
				ps := subgroup_info.NewPodSet(DefaultSubGroup, 2, nil)
				ps.AssignTask(simpleTask("pod-a", "", pod_status.Running))
				ps.AssignTask(simpleTask("pod-b", "", pod_status.Running))
				ps.AssignTask(simpleTask("pod-c", "", pod_status.Running))
				ps.AssignTask(simpleTask("pod-d", "", pod_status.Running))
				return &PodGroupInfo{PodSets: map[string]*subgroup_info.PodSet{DefaultSubGroup: ps}}
			}(),
			expectedCoreCount: 2,
			expectedCoreNames: []string{"pod-a", "pod-b"},
		},
		{
			name:              "MinMemberZero_NoCore",
			job:               segmentedJob(nil, 1, 0, []int{3}),
			expectedCoreCount: 0,
		},
		{
			name:              "Segmented_MinSubGroupBelowCount_TwoSegmentsCore",
			job:               segmentedJob(ptr.To(int32(2)), 4, 2, []int{2, 2, 2, 2}),
			expectedCoreCount: 4,
			// whole-segment integrity: exactly segment-0 and segment-1 are core, never a half segment
			expectedCoreNames: []string{"segment-0-p0", "segment-0-p1", "segment-1-p0", "segment-1-p1"},
		},
		{
			name:              "Segmented_MinSubGroupEqualsCount_AllCore",
			job:               segmentedJob(ptr.To(int32(4)), 4, 2, []int{2, 2, 2, 2}),
			expectedCoreCount: 8,
		},
		{
			name:              "MinSubGroupUnset_AllChildrenCore",
			job:               segmentedJob(nil, 2, 2, []int{2, 2}),
			expectedCoreCount: 4,
		},
		{
			name:              "LeafSurplusExcluded_OnlyMinMemberPerRetainedSegment",
			job:               segmentedJob(ptr.To(int32(2)), 4, 1, []int{3, 3, 3, 3}),
			expectedCoreCount: 2,
			expectedCoreNames: []string{"segment-0-p0", "segment-1-p0"},
		},
		{
			name:              "GangNotMet_AllAllocatedAreCore",
			job:               segmentedJob(ptr.To(int32(2)), 4, 2, []int{2, 1, 0, 0}),
			expectedCoreCount: 3,
			expectedCoreNames: []string{"segment-0-p0", "segment-0-p1", "segment-1-p0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := GetCoreTasks(tt.job, subGroupMemberOrderFn, tasksOrderFn)
			assert.Equal(t, tt.expectedCoreCount, len(core))
			if tt.expectedCoreNames != nil {
				names := make([]string, 0, len(core))
				for _, task := range core {
					names = append(names, task.Name)
				}
				assert.ElementsMatch(t, tt.expectedCoreNames, names)
			}
		})
	}
}
