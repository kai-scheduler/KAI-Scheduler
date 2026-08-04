// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"fmt"
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
		{
			// A partially-filled subgroup must not take a core slot ahead of a complete one. The
			// allocation ordering ranks the unsatisfied A first, which would protect A's orphan pod
			// (useless on its own) and leave the complete C evictable.
			name: "PartialSubGroupDoesNotStealCoreSlot",
			job: func() *PodGroupInfo {
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(2)))
				for _, sg := range []struct {
					name      string
					allocated int
				}{{"a", 1}, {"b", 2}, {"c", 2}} {
					ps := subgroup_info.NewPodSet(sg.name, 2, nil)
					for i := 0; i < sg.allocated; i++ {
						ps.AssignTask(simpleTask(fmt.Sprintf("%s-p%d", sg.name, i), sg.name, pod_status.Running))
					}
					root.AddPodSet(ps)
				}
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			expectedCoreNames: []string{"b-p0", "b-p1", "c-p0", "c-p1"},
			expectedMinSat:    true,
		},
		{
			// An emptied subgroup is maximally unsatisfied, so the allocation ordering ranks it first
			// and hands it a core slot holding zero pods — pushing a real subgroup out to elastic.
			name: "EmptySubGroupNeverTakesCoreSlot",
			job: func() *PodGroupInfo {
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(2)))
				for _, sg := range []struct {
					name      string
					allocated int
				}{{"r0", 2}, {"r1", 2}, {"r2", 2}, {"r3", 0}} {
					ps := subgroup_info.NewPodSet(sg.name, 2, nil)
					for i := 0; i < sg.allocated; i++ {
						ps.AssignTask(simpleTask(fmt.Sprintf("%s-p%d", sg.name, i), sg.name, pod_status.Running))
					}
					root.AddPodSet(ps)
				}
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			expectedCoreNames: []string{"r0-p0", "r0-p1", "r1-p0", "r1-p1"},
			expectedMinSat:    true,
		},
		{
			// Nothing is satisfied yet: fill the slots from the unsatisfied members by name so an
			// assembling gang still protects what it has landed.
			name: "RampUp_FillsFromUnsatisfiedByName",
			job: func() *PodGroupInfo {
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(2)))
				for _, name := range []string{"a", "b"} {
					ps := subgroup_info.NewPodSet(name, 2, nil)
					ps.AssignTask(simpleTask(name+"-p0", name, pod_status.Running))
					root.AddPodSet(ps)
				}
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			expectedCoreNames: []string{"a-p0", "b-p0"},
			expectedMinSat:    false,
		},
		{
			// Two levels: the rule applies per-SubGroupSet, so a core subgroup protects only its own
			// core children — x2/x3 stay elastic even though X itself is core.
			name: "TwoLevelTree_CorePerSubGroupSet",
			job: func() *PodGroupInfo {
				root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				root.SetMinSubGroup(ptr.To(int32(2)))

				x := subgroup_info.NewSubGroupSet("x", nil)
				x.SetMinSubGroup(ptr.To(int32(2)))
				for _, name := range []string{"x0", "x1", "x2", "x3"} {
					ps := subgroup_info.NewPodSet(name, 1, nil)
					ps.AssignTask(simpleTask(name+"-p0", name, pod_status.Running))
					x.AddPodSet(ps)
				}
				root.AddSubGroup(x)

				for _, name := range []string{"y", "z"} {
					ps := subgroup_info.NewPodSet(name, 1, nil)
					ps.AssignTask(simpleTask(name+"-p0", name, pod_status.Running))
					root.AddPodSet(ps)
				}
				return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
			}(),
			// Root core = x, y (z elastic); inside x, core = x0, x1 (x2, x3 elastic).
			expectedCoreNames: []string{"x0-p0", "x1-p0", "y-p0"},
			expectedMinSat:    true,
		},
	}

	t.Run("GetCorePodNames_SortedForStableComparison", func(t *testing.T) {
		// 4 fully-gang subgroups, minSubGroup=2 → r0 and r1 are core, names returned sorted.
		root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
		root.SetMinSubGroup(ptr.To(int32(2)))
		for _, name := range []string{"r0", "r1", "r2", "r3"} {
			ps := subgroup_info.NewPodSet(name, 2, nil)
			ps.AssignTask(simpleTask(name+"-p1", name, pod_status.Running))
			ps.AssignTask(simpleTask(name+"-p0", name, pod_status.Running))
			root.AddPodSet(ps)
		}
		job := &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}

		assert.Equal(t,
			[]string{"r0-p0", "r0-p1", "r1-p0", "r1-p1"},
			GetCorePodNames(job, tasksOrderFn))
	})

	// The core must not move when an elastic subgroup is evicted. If it did, the subgroup that just
	// lost its pods would rank first (most unsatisfied), take a core slot holding nothing, and push a
	// live subgroup out to elastic — evict again and the job unravels one subgroup at a time until
	// every real pod is elastic and the "core" is empty.
	t.Run("CoreIsStickyAcrossEviction", func(t *testing.T) {
		// allocatedPerSubGroup builds the 4-replica shape with the given pod counts, as the next
		// session would rebuild it from live cluster state.
		buildJob := func(allocated map[string]int) *PodGroupInfo {
			root := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
			root.SetMinSubGroup(ptr.To(int32(2)))
			for _, name := range []string{"r0", "r1", "r2", "r3"} {
				ps := subgroup_info.NewPodSet(name, 2, nil)
				for i := 0; i < allocated[name]; i++ {
					ps.AssignTask(simpleTask(fmt.Sprintf("%s-p%d", name, i), name, pod_status.Running))
				}
				root.AddPodSet(ps)
			}
			return &PodGroupInfo{RootSubGroupSet: root, PodSets: root.GetDescendantPodSets()}
		}

		expected := []string{"r0-p0", "r0-p1", "r1-p0", "r1-p1"}
		full := buildJob(map[string]int{"r0": 2, "r1": 2, "r2": 2, "r3": 2})
		assert.Equal(t, expected, GetCorePodNames(full, tasksOrderFn))

		// r3 evicted as a unit — core must not follow it.
		afterFirst := buildJob(map[string]int{"r0": 2, "r1": 2, "r2": 2, "r3": 0})
		assert.Equal(t, expected, GetCorePodNames(afterFirst, tasksOrderFn),
			"core moved onto the emptied r3")

		// r2 evicted too — the job is down to exactly its core, which is still the same two subgroups.
		afterSecond := buildJob(map[string]int{"r0": 2, "r1": 2, "r2": 0, "r3": 0})
		assert.Equal(t, expected, GetCorePodNames(afterSecond, tasksOrderFn),
			"core unravelled onto the emptied subgroups")
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := GetCoreTasks(tt.job, tasksOrderFn)
			gotNames := make([]string, 0, len(core))
			for _, task := range core {
				gotNames = append(gotNames, task.Name)
			}
			assert.ElementsMatch(t, tt.expectedCoreNames, gotNames)
			assert.Equal(t, tt.expectedMinSat, IsMinRequirementSatisfied(tt.job))
		})
	}
}
