// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

func Test_getMaxNumOfTasksToAllocate(t *testing.T) {
	type args struct {
		minAvailable          int32
		pods                  []*v1.Pod
		podsWantingToAllocate int
		overridingStatus      []pod_status.PodStatus
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "single pod pending",
			args: args{
				minAvailable: 1,
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				podsWantingToAllocate: 1,
			},
			want: 1,
		},
		{
			name: "single pod running",
			args: args{
				minAvailable: 1,
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				podsWantingToAllocate: 0,
			},
			want: 0,
		},
		{
			name: "three pods pending",
			args: args{
				minAvailable: 3,
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p2", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p3", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				podsWantingToAllocate: 3,
			},
			want: 3,
		},
		{
			name: "four pods, min available equal running, two pending",
			args: args{
				minAvailable: 2,
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p2", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p3", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p4", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				podsWantingToAllocate: 2,
			},
			want: 1,
		},
		{
			name: "pipline over dying pods",
			args: args{
				minAvailable: 2,
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p2", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p3", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p4", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				overridingStatus: []pod_status.PodStatus{pod_status.Releasing, pod_status.Releasing,
					pod_status.Pending, pod_status.Pending},
				podsWantingToAllocate: 2,
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := NewPodGroupInfo("u1")
			pg.MinAvailable = tt.args.minAvailable
			for i, pod := range tt.args.pods {
				pi := pod_info.NewTaskInfo(pod)
				pg.AddTaskInfo(pi)

				if tt.args.overridingStatus != nil {
					pi.Status = tt.args.overridingStatus[i]
				}
			}

			if got := getNumOfTasksToAllocate(pg, tt.args.podsWantingToAllocate); got != tt.want {
				t.Errorf("getNumOfTasksToAllocate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNumOfAllocatedTasks(t *testing.T) {
	type args struct {
		pods             []*v1.Pod
		overridingStatus []pod_status.PodStatus
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "single pod pending",
			args: args{
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
			},
			want: 0,
		},
		{
			name: "single pod running",
			args: args{
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
			},
			want: 1,
		},
		{
			name: "single pod releasing",
			args: args{
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodFailed,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
				overridingStatus: []pod_status.PodStatus{pod_status.Releasing},
			},
			want: 0,
		},
		{
			name: "two pods running",
			args: args{
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p2", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
			},
			want: 2,
		},
		{
			name: "one pending one running",
			args: args{
				pods: []*v1.Pod{
					common_info.BuildPod("n1", "p1", "", v1.PodPending,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
					common_info.BuildPod("n1", "p2", "", v1.PodRunning,
						common_info.BuildResourceList("1000m", "1G"),
						nil, nil, nil),
				},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := NewPodGroupInfo("u1")
			for i, pod := range tt.args.pods {
				pi := pod_info.NewTaskInfo(pod)
				pg.AddTaskInfo(pi)

				if tt.args.overridingStatus != nil {
					pi.Status = tt.args.overridingStatus[i]
				}
			}

			if got := getNumOfAllocatedTasks(pg); got != tt.want {
				t.Errorf("getNumOfAllocatedTasks() = %v, want %v", got, tt.want)
			}
		})
	}
}
