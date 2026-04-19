// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
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
				SubGroups: map[string]*SubGroupInfo{
					DefaultSubGroup: NewSubGroupInfo(DefaultSubGroup, 1).WithPodInfos(pod_info.PodsMap{
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
				SubGroups: map[string]*SubGroupInfo{
					DefaultSubGroup: NewSubGroupInfo(DefaultSubGroup, 1),
				},
			},
			expectedHasMoreTasks: false,
			numExpectTasks:       0,
		},
		{
			name: "WithoutSubGroups_MultipleEvict",
			job: &PodGroupInfo{
				SubGroups: map[string]*SubGroupInfo{
					DefaultSubGroup: NewSubGroupInfo(DefaultSubGroup, 2).WithPodInfos(pod_info.PodsMap{
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
				pg.SetDefaultMinAvailable(2)
				pg.SubGroups["sg1"] = NewSubGroupInfo("sg1", 1)
				pg.SubGroups["sg2"] = NewSubGroupInfo("sg2", 1)

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
				sub1 := NewSubGroupInfo("sg1", 1)
				sub1.AssignTask(simpleTask("pod-1", "sg1", pod_status.Running))

				sub2 := NewSubGroupInfo("sg2", 1)
				sub2.AssignTask(simpleTask("pod-2", "sg2", pod_status.Running))

				return &PodGroupInfo{
					SubGroups: map[string]*SubGroupInfo{
						DefaultSubGroup: NewSubGroupInfo(DefaultSubGroup, 2),
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
