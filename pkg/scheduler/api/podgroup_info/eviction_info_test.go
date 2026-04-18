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

func subGroupMemberOrderFn(l, r interface{}) bool {
	lMember := l.(subgroup_info.SubGroupMember)
	rMember := r.(subgroup_info.SubGroupMember)
	return lMember.GetName() < rMember.GetName()
}

func TestGetTasksToEvict_Table(t *testing.T) {
	tests := []struct {
		name                 string
		job                  *PodGroupInfo
		expectedHasMoreTasks bool
		numExpectTasks       int
	}{
		{
			name: "WithoutSubGroups_EvictOne",
			job: &PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 1, nil).WithPodInfos(pod_info.PodsMap{
						"pod-a": simpleTask("pod-a", "", pod_status.Running),
						"pod-b": simpleTask("pod-b", "", pod_status.Running),
						"pod-c": simpleTask("pod-c", "", pod_status.Running),
					}),
				},
			},
			expectedHasMoreTasks: true,
			numExpectTasks:       1,
		},
		{
			name: "WithoutSubGroups_EmptyQueue",
			job: &PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 1, nil),
				},
			},
			expectedHasMoreTasks: false,
			numExpectTasks:       0,
		},
		{
			name: "WithoutSubGroups_MultipleEvict",
			job: &PodGroupInfo{
				PodSets: map[string]*subgroup_info.PodSet{
					DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"pod-a": simpleTask("pod-a", "", pod_status.Running),
							"pod-b": simpleTask("pod-b", "", pod_status.Running),
						}),
				},
			},
			expectedHasMoreTasks: false,
			numExpectTasks:       2,
		},
		{
			name: "WithSubGroups_SingleEvict",
			job: func() *PodGroupInfo {
				sg1 := subgroup_info.NewPodSet("sg1", 1, nil)
				sg1.AssignTask(simpleTask("pod-1", "sg1", pod_status.Running))
				sg1.AssignTask(simpleTask("pod-2", "sg1", pod_status.Running))

				sg2 := subgroup_info.NewPodSet("sg2", 1, nil)
				sg2.AssignTask(simpleTask("pod-3", "sg2", pod_status.Running))

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(sg1)
				root.AddPodSet(sg2)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			expectedHasMoreTasks: true,
			numExpectTasks:       1,
		},
		{
			name: "WithSubGroups_EvictAll",
			job: func() *PodGroupInfo {
				sub1 := subgroup_info.NewPodSet("sg1", 1, nil)
				sub1.AssignTask(simpleTask("pod-1", "sg1", pod_status.Running))

				sub2 := subgroup_info.NewPodSet("sg2", 1, nil)
				sub2.AssignTask(simpleTask("pod-2", "sg2", pod_status.Running))

				return &PodGroupInfo{
					PodSets: map[string]*subgroup_info.PodSet{
						DefaultSubGroup: subgroup_info.NewPodSet(DefaultSubGroup, 2, nil),
						"sg1":           sub1,
						"sg2":           sub2,
					},
				}
			}(),
			expectedHasMoreTasks: false,
			numExpectTasks:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasksToEvict, hasMoreTasks := GetTasksToEvict(tt.job, subGroupOrderFn, tasksOrderFn)
			assert.Equal(t, tt.expectedHasMoreTasks, hasMoreTasks)
			assert.Equal(t, tt.numExpectTasks, len(tasksToEvict))
		})
	}
}

func TestGetTasksToEvict_HierarchicalTree(t *testing.T) {
	tests := []struct {
		name                 string
		job                  *PodGroupInfo
		expectedHasMoreTasks bool
		numExpectTasks       int
		expectedTaskNames    []string
	}{
		{
			name: "Phase1_ElasticRecursive_EvictFromPodSetAboveMin",
			job: func() *PodGroupInfo {
				// Inner SubGroupSet with 2 PodSets: ps-a at min, ps-b above min
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))

				psB := subgroup_info.NewPodSet("ps-b", 1, nil)
				psB.AssignTask(simpleTask("pod-2", "ps-b", pod_status.Running))
				psB.AssignTask(simpleTask("pod-3", "ps-b", pod_status.Running))

				inner := subgroup_info.NewSubGroupSet("inner", nil)
				inner.AddPodSet(psA)
				inner.AddPodSet(psB)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddSubGroup(inner)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			expectedHasMoreTasks: true,
			numExpectTasks:       1,
		},
		{
			name: "Phase2_ElasticDirect_DropMemberWhenMinSubGroupLessThanSatisfied",
			job: func() *PodGroupInfo {
				// Root with minSubGroup=1, 2 members PodSets both at their min
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))

				psB := subgroup_info.NewPodSet("ps-b", 1, nil)
				psB.AssignTask(simpleTask("pod-2", "ps-b", pod_status.Running))

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(1)))
				root.AddPodSet(psA)
				root.AddPodSet(psB)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// Phase 2: drop least-prioritized member (ps-b), evict all its tasks
			expectedHasMoreTasks: true,
			numExpectTasks:       1,
		},
		{
			name: "Phase2_ElasticDirect_NestedSubGroupSetWithMinSubGroup",
			job: func() *PodGroupInfo {
				// Root has member SubGroupSet "inner" with minSubGroup=1 and 2 PodSet members at min
				psA := subgroup_info.NewPodSet("ps-a", 2, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))
				psA.AssignTask(simpleTask("pod-2", "ps-a", pod_status.Running))

				psB := subgroup_info.NewPodSet("ps-b", 2, nil)
				psB.AssignTask(simpleTask("pod-3", "ps-b", pod_status.Running))
				psB.AssignTask(simpleTask("pod-4", "ps-b", pod_status.Running))

				inner := subgroup_info.NewSubGroupSet("inner", nil)
				inner.SetMinSubGroup(ptr.To(int32(1)))
				inner.AddPodSet(psA)
				inner.AddPodSet(psB)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddSubGroup(inner)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// Phase 1 recurses into inner → inner's phase 2 drops ps-b (2 tasks)
			expectedHasMoreTasks: true,
			numExpectTasks:       2,
		},
		{
			name: "Phase3_FullSubtreeEviction_AllMembersAtMin",
			job: func() *PodGroupInfo {
				// Root needs all members (no minSubGroup), both at min
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))

				psB := subgroup_info.NewPodSet("ps-b", 1, nil)
				psB.AssignTask(simpleTask("pod-2", "ps-b", pod_status.Running))

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(psA)
				root.AddPodSet(psB)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// No elastic surplus, numSatisfied == K → phase 3: evict all
			expectedHasMoreTasks: false,
			numExpectTasks:       2,
		},
		{
			name: "NotSatisfied_ReturnsNil",
			job: func() *PodGroupInfo {
				// Root needs all members, but ps-b has no allocated tasks
				psA := subgroup_info.NewPodSet("ps-a", 1, nil)
				psA.AssignTask(simpleTask("pod-1", "ps-a", pod_status.Running))

				psB := subgroup_info.NewPodSet("ps-b", 1, nil)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(psA)
				root.AddPodSet(psB)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// ps-b is not satisfied, root numSatisfied=1 < K=2
			// Phase 1: ps-a has elastic surplus? No (1 == min 1). ps-b has 0.
			// Phase 2: K=2, numSatisfied=1, condition false
			// Phase 3: evict all → pod-1
			expectedHasMoreTasks: false,
			numExpectTasks:       1,
		},
		{
			name: "MixedSubGroupSetAndPodSetMembers",
			job: func() *PodGroupInfo {
				// Root has a PodSet member and a SubGroupSet member
				psRoot := subgroup_info.NewPodSet("ps-root", 1, nil)
				psRoot.AssignTask(simpleTask("pod-1", "ps-root", pod_status.Running))
				psRoot.AssignTask(simpleTask("pod-2", "ps-root", pod_status.Running))

				psInner := subgroup_info.NewPodSet("ps-inner", 1, nil)
				psInner.AssignTask(simpleTask("pod-3", "ps-inner", pod_status.Running))

				inner := subgroup_info.NewSubGroupSet("inner", nil)
				inner.AddPodSet(psInner)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.AddPodSet(psRoot)
				root.AddSubGroup(inner)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// ps-root has elastic surplus (2 > 1), evict 1 task from it
			expectedHasMoreTasks: true,
			numExpectTasks:       1,
		},
		{
			name: "Phase1_GangEvictionInsideMember_DuringElasticRecursion",
			job: func() *PodGroupInfo {
				// Root with minSubGroup=1, 2 SubGroupSet members
				// member-a: minSubGroup=nil (needs all), 2 PodSets at min → no elastic surplus
				// member-b: minSubGroup=nil (needs all), 2 PodSets at min → no elastic surplus
				// Root has numSatisfied=2 > K=1 → phase 1 recurses into member-b (least priority)
				// member-b has no elastic surplus → phase 3 evicts ALL from member-b

				psA1 := subgroup_info.NewPodSet("ps-a1", 1, nil)
				psA1.AssignTask(simpleTask("pod-1", "ps-a1", pod_status.Running))
				psA2 := subgroup_info.NewPodSet("ps-a2", 1, nil)
				psA2.AssignTask(simpleTask("pod-2", "ps-a2", pod_status.Running))
				memberA := subgroup_info.NewSubGroupSet("member-a", nil)
				memberA.AddPodSet(psA1)
				memberA.AddPodSet(psA2)

				psB1 := subgroup_info.NewPodSet("ps-b1", 1, nil)
				psB1.AssignTask(simpleTask("pod-3", "ps-b1", pod_status.Running))
				psB2 := subgroup_info.NewPodSet("ps-b2", 1, nil)
				psB2.AssignTask(simpleTask("pod-4", "ps-b2", pod_status.Running))
				memberB := subgroup_info.NewSubGroupSet("member-b", nil)
				memberB.AddPodSet(psB1)
				memberB.AddPodSet(psB2)

				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(1)))
				root.AddSubGroup(memberA)
				root.AddSubGroup(memberB)

				return &PodGroupInfo{
					RootSubGroupSet: root,
					PodSets:         root.GetDescendantPodSets(),
				}
			}(),
			// Root phase 1: hasElasticSurplus → numSatisfied(2) > K(1) → true
			// Iterate: member-b (reversed, "member-b" > "member-a")
			// member-b elastic: no surplus → returns nil
			// member-a elastic: no surplus → returns nil
			// Root phase 2: K(1) < numSatisfied(2) → true
			// collectGangEviction from member-b → ALL 2 tasks
			expectedHasMoreTasks: true,
			numExpectTasks:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasksToEvict, hasMoreTasks := GetTasksToEvict(tt.job, subGroupMemberOrderFn, tasksOrderFn)
			assert.Equal(t, tt.expectedHasMoreTasks, hasMoreTasks)
			assert.Equal(t, tt.numExpectTasks, len(tasksToEvict))
			if tt.expectedTaskNames != nil {
				gotTaskNames := make([]string, 0, len(tasksToEvict))
				for _, task := range tasksToEvict {
					gotTaskNames = append(gotTaskNames, task.Name)
				}
				assert.ElementsMatch(t, tt.expectedTaskNames, gotTaskNames)
			}
		})
	}
}
