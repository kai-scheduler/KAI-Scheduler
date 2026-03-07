// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
)

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
				pg := NewPodGroupInfo("pg1")
				pg.GetSubGroups()[DefaultSubGroup].SetMinAvailable(2)
				pg.PodSets["sg1"] = subgroup_info.NewPodSet("sg1", 1, nil)
				pg.PodSets["sg2"] = subgroup_info.NewPodSet("sg2", 1, nil)

				pg.AddTaskInfo(simpleTask("pod-1", "sg1", pod_status.Running))
				pg.AddTaskInfo(simpleTask("pod-2", "sg1", pod_status.Running))
				pg.AddTaskInfo(simpleTask("pod-3", "sg2", pod_status.Running))
				return pg
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
