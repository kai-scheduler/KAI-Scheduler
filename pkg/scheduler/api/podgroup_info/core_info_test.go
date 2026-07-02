// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

func TestGetCoreTasks(t *testing.T) {
	tests := []struct {
		name              string
		job               *PodGroupInfo
		expectedCoreNames []string
		expectedMinSat    bool
	}{
		{
			name: "FlatJob_EqualsLeafMinMember",
			job: &PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 2, nil).WithPodInfos(pod_info.PodsMap{
						"pod-a": simpleTask("pod-a", "", pod_status.Running),
						"pod-b": simpleTask("pod-b", "", pod_status.Running),
						"pod-c": simpleTask("pod-c", "", pod_status.Running),
					}),
				},
			},
			// minMember=2, three allocated → 2 core (lowest-UID first: pod-a, pod-b)
			expectedCoreNames: []string{"pod-a", "pod-b"},
			expectedMinSat:    true,
		},
		{
			name: "MinMemberZero_NoneCore",
			job: &PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 0, nil).WithPodInfos(pod_info.PodsMap{
						"pod-a": simpleTask("pod-a", "", pod_status.Running),
						"pod-b": simpleTask("pod-b", "", pod_status.Running),
					}),
				},
			},
			expectedCoreNames: []string{},
			expectedMinSat:    true,
		},
		{
			name: "MinSubGroupLessThanChildren",
			job: func() *PodGroupInfo {
				// Root minSubGroup=1 over two leaf PodSets each at min → 1 core subgroup (ps-a).
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))
				psB := subgroup_info.NewPodSet("ps-b", 1, nil)
				psB.AssignTask(simpleTask("pod-2", "ps-b", pod_status.Running))

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(1)))
				root.AddPodSet(psA)
				root.AddPodSet(psB)
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			expectedCoreNames: []string{"pod-1"},
			expectedMinSat:    true,
		},
		{
			name: "SegmentedShape_MinSubGroup2Of4",
			job: func() *PodGroupInfo {
				// 4 fully-gang subgroups (each a leaf PodSet, minMember=2), minSubGroup=2 → 2 core subgroups (4 pods).
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(2)))
				for _, name := range []string{"r0", "r1", "r2", "r3"} {
					ps := subgroup_info.NewPodSet(name, 2, nil)
					ps.AssignTask(simpleTask(name+"-p0", name, pod_status.Running))
					ps.AssignTask(simpleTask(name+"-p1", name, pod_status.Running))
					root.AddPodSet(ps)
				}
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			// 2 highest-priority (lowest-name) satisfied subgroups: r0, r1 → 4 pods
			expectedCoreNames: []string{"r0-p0", "r0-p1", "r1-p0", "r1-p1"},
			expectedMinSat:    true,
		},
		{
			name: "MinSubGroupUnset_AllCore",
			job: func() *PodGroupInfo {
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))
				psB := subgroup_info.NewPodSet("ps-b", 1, nil)
				psB.AssignTask(simpleTask("pod-2", "ps-b", pod_status.Running))

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(psA)
				root.AddPodSet(psB)
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			expectedCoreNames: []string{"pod-1", "pod-2"},
			expectedMinSat:    true,
		},
		{
			name: "NotSatisfied_MinNotMet",
			job: func() *PodGroupInfo {
				// Root needs both children, but ps-b has no allocated tasks.
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))
				psB := subgroup_info.NewPodSet("ps-b", 1, nil)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(psA)
				root.AddPodSet(psB)
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			// ps-a is satisfied and counts toward core; ps-b unsatisfied contributes nothing.
			expectedCoreNames: []string{"pod-1"},
			expectedMinSat:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := GetCoreTasks(tt.job, subGroupMemberOrderFn, tasksOrderFn)
			gotNames := make([]string, 0, len(core))
			for _, task := range core {
				gotNames = append(gotNames, task.Name)
			}
			assert.ElementsMatch(t, tt.expectedCoreNames, gotNames)
			assert.Equal(t, tt.expectedMinSat, IsMinRequirementSatisfied(tt.job))
		})
	}
}
