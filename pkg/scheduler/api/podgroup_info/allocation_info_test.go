// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/scheduler_util"
)

func simpleTask(name string, subGroupName string, status pod_status.PodStatus) *pod_info.PodInfo {
	pod := common_info.BuildPod("test-namespace", name, "", v1.PodPending,
		common_info.BuildResourceList("1", "1G"),
		nil, nil, nil,
	)
	info := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
	info.Status = status
	info.SubGroupName = subGroupName
	return info
}

func tasksOrderFn(l, r interface{}) bool {
	lTask := l.(*pod_info.PodInfo)
	rTask := r.(*pod_info.PodInfo)
	return lTask.UID < rTask.UID
}

func subGroupOrderFn(l, r interface{}) bool {
	lSubGroup := l.(*subgroup_info.PodSet)
	rSubGroup := r.(*subgroup_info.PodSet)
	return lSubGroup.GetName() < rSubGroup.GetName()
}

func Test_HasTasksToAllocate(t *testing.T) {
	pg := NewPodGroupInfo("pg1")
	if HasTasksToAllocate(pg, true) {
		t.Error("expected false with zero tasks")
	}
	// Add one pending that ShouldAllocate
	task := simpleTask("p1", "", pod_status.Pending)
	pg.AddTaskInfo(task)
	if !HasTasksToAllocate(pg, true) {
		t.Error("expected true with allocatable task")
	}
	// Now set the status so ShouldAllocate returns false
	task.Status = pod_status.Succeeded
	if HasTasksToAllocate(pg, true) {
		t.Error("expected false with non-allocatable status")
	}
}

func Test_GetTasksToAllocate(t *testing.T) {
	type testCase struct {
		name          string
		subGroupTasks map[string][]*pod_info.PodInfo
		minAvailMap   map[string]int32
		wantTasks     []string
		wantNumTasks  int
	}
	tests := []testCase{
		{
			name: "single pending task",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup1": {
					simpleTask("task1", "subGroup1", pod_status.Pending),
				},
			},
			minAvailMap:  map[string]int32{"subGroup1": 1},
			wantTasks:    []string{"task1"},
			wantNumTasks: 1,
		},
		{
			name: "multiple pending tasks",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup2": {
					simpleTask("task1", "subGroup2", pod_status.Pending),
					simpleTask("task2", "subGroup2", pod_status.Pending),
				},
			},
			minAvailMap:  map[string]int32{"subGroup2": 2},
			wantTasks:    []string{"task1", "task2"},
			wantNumTasks: 2,
		},
		{
			name: "one allocated and one pending",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup3": {
					simpleTask("task1", "subGroup3", pod_status.Allocated),
					simpleTask("task2", "subGroup3", pod_status.Pending),
				},
			},
			minAvailMap:  map[string]int32{"subGroup3": 1},
			wantTasks:    []string{"task2"},
			wantNumTasks: 1,
		},
		{
			name: "pending in multiple subgroups, subGroups below minAvailable",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup1": {
					simpleTask("task1", "subGroup1", pod_status.Pending),
				},
				"subGroup2": {
					simpleTask("task2", "subGroup2", pod_status.Pending),
				},
			},
			minAvailMap:  map[string]int32{"subGroup1": 1, "subGroup2": 1},
			wantTasks:    []string{"task1", "task2"},
			wantNumTasks: 2,
		},
		{
			name: "no allocatable tasks",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup4": {
					simpleTask("task1", "subGroup4", pod_status.Allocated),
				},
			},
			minAvailMap:  map[string]int32{"subGroup4": 1},
			wantTasks:    []string{},
			wantNumTasks: 0,
		},
		{
			name: "two subgroups, allocation left only in second",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup1": {
					simpleTask("task1", "subGroup1", pod_status.Running),
				},
				"subGroup2": {
					simpleTask("task2", "subGroup2", pod_status.Running),
					simpleTask("task3", "subGroup2", pod_status.Pending),
				},
			},
			minAvailMap: map[string]int32{
				"subGroup1": 1,
				"subGroup2": 1,
			},
			wantTasks:    []string{"task3"},
			wantNumTasks: 1,
		},
		{
			name: "three subgroups, last two are not gang satisfied",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup1": {
					simpleTask("task1", "subGroup1", pod_status.Running),
				},
				"subGroup2": {
					simpleTask("task2", "subGroup2", pod_status.Pending),
				},
				"subGroup3": {
					simpleTask("task3", "subGroup3", pod_status.Pending),
				},
			},
			minAvailMap: map[string]int32{
				"subGroup1": 1,
				"subGroup2": 1,
				"subGroup3": 1,
			},
			wantTasks:    []string{"task2", "task3"},
			wantNumTasks: 2,
		},
		{
			name: "three subgroups, all gang satisfied, allocation left in the last two",
			subGroupTasks: map[string][]*pod_info.PodInfo{
				"subGroup1": {
					simpleTask("task1", "subGroup1", pod_status.Running),
				},
				"subGroup2": {
					simpleTask("task2", "subGroup2", pod_status.Running),
					simpleTask("task3", "subGroup2", pod_status.Pending),
				},
				"subGroup3": {
					simpleTask("task4", "subGroup3", pod_status.Running),
					simpleTask("task5", "subGroup3", pod_status.Pending),
				},
			},
			minAvailMap: map[string]int32{
				"subGroup1": 1,
				"subGroup2": 1,
				"subGroup3": 1,
			},
			wantTasks:    []string{"task3"},
			wantNumTasks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := NewPodGroupInfo("pg")
			for subGroupName, pods := range tt.subGroupTasks {
				if _, exists := pg.GetSubGroups()[subGroupName]; !exists {
					pg.PodSets[subGroupName] = subgroup_info.NewPodSet(subGroupName, tt.minAvailMap[subGroupName], nil)
				}
				for _, pod := range pods {
					pg.AddTaskInfo(pod)
				}
			}
			gotTasks := GetTasksToAllocate(pg, subGroupOrderFn, tasksOrderFn, true)
			if len(gotTasks) != tt.wantNumTasks {
				t.Errorf("expected %d tasks to allocate, got %d", tt.wantNumTasks, len(gotTasks))
			}
			for i, want := range tt.wantTasks {
				if i < len(gotTasks) && gotTasks[i].Pod.Name != want {
					t.Errorf("at %d: want task name=%q, got=%q", i, want, gotTasks[i].Pod.Name)
				}
			}
		})
	}
}

func Test_GetTasksToAllocateRequestedGPUs(t *testing.T) {
	pg := NewPodGroupInfo("test-podgroup")
	pg.GetSubGroups()[DefaultSubGroup].SetMinAvailable(1)
	task := simpleTask("p1", "", pod_status.Pending)
	// manually set up a fake ResReq that returns 2 for GPUs and 1000 for GpuMemory
	task.ResReq = resource_info.NewResourceRequirements(2, 1000, 2000)
	pg.AddTaskInfo(task)
	gpus, _ := GetTasksToAllocateRequestedGPUs(pg, subGroupOrderFn, tasksOrderFn, true)
	if gpus != 2 {
		t.Errorf("expected gpus=2, got %v", gpus)
	}
}

func Test_GetTasksToAllocateInitResource(t *testing.T) {
	pg := NewPodGroupInfo("ri")
	// Nil case
	res := GetTasksToAllocateInitResource(nil, subGroupOrderFn, tasksOrderFn, true, 0)
	if !res.IsEmpty() {
		t.Error("empty resource expected for nil pg")
	}

	pg.GetSubGroups()[DefaultSubGroup].SetMinAvailable(1)
	task := simpleTask("p", "", pod_status.Pending)
	task.ResReq = resource_info.NewResourceRequirements(0, 5000, 0)
	pg.AddTaskInfo(task)
	resource := GetTasksToAllocateInitResource(pg, subGroupOrderFn, tasksOrderFn, true, 0)
	cpu := resource.BaseResource.Get(v1.ResourceCPU)
	if cpu != 5000 {
		t.Fatalf("want cpu=5, got %v", cpu)
	}
	// Memoization/second call should return r
	newResource := GetTasksToAllocateInitResource(pg, subGroupOrderFn, tasksOrderFn, true, 0)
	if newResource != resource {
		t.Error("cached resource pointer mismatch")
	}
}

func Test_getTasksFromQueue(t *testing.T) {
	type testCase struct {
		name        string
		podNames    []string
		maxNumTasks int
		wantTasks   []string
	}
	tests := []testCase{
		{
			name:        "get one from queue with two tasks",
			podNames:    []string{"task1", "task2"},
			maxNumTasks: 1,
			wantTasks:   []string{"task1"},
		},
		{
			name:        "get all from queue (less than maxNumTasks)",
			podNames:    []string{"task1", "task2"},
			maxNumTasks: 5,
			wantTasks:   []string{"task1", "task2"},
		},
		{
			name:        "get all from queue (exact limit)",
			podNames:    []string{"task1", "task2", "task3"},
			maxNumTasks: 3,
			wantTasks:   []string{"task1", "task2", "task3"},
		},
		{
			name:        "get zero from empty queue",
			podNames:    []string{},
			maxNumTasks: 2,
			wantTasks:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := scheduler_util.NewPriorityQueue(tasksOrderFn, 10)
			for _, name := range tt.podNames {
				q.Push(simpleTask(name, "", pod_status.Pending))
			}
			tasks := getTasksFromQueue(q, tt.maxNumTasks)
			if len(tasks) != len(tt.wantTasks) {
				t.Errorf("expected %d tasks from queue, got %d", len(tt.wantTasks), len(tasks))
			}
			for i, want := range tt.wantTasks {
				if i < len(tasks) && tasks[i].Pod.Name != want {
					t.Errorf("at %d: want task name=%q, got=%q", i, want, tasks[i].Pod.Name)
				}
			}
		})
	}
}

func Test_getTasksPriorityQueue(t *testing.T) {
	tests := []struct {
		name              string
		tasks             []*pod_info.PodInfo
		isRealAllocation  bool
		wantLen           int
		wantFirstTaskName string
	}{
		{
			name: "one pending task",
			tasks: []*pod_info.PodInfo{
				simpleTask("task1", "subGroup1", pod_status.Pending),
			},
			isRealAllocation:  true,
			wantLen:           1,
			wantFirstTaskName: "task1",
		},
		{
			name: "one allocated and one pending task, only allocatable",
			tasks: []*pod_info.PodInfo{
				simpleTask("task1", "subGroup1", pod_status.Allocated),
				simpleTask("task2", "subGroup1", pod_status.Pending),
			},
			isRealAllocation:  true,
			wantLen:           1,
			wantFirstTaskName: "task2",
		},
		{
			name: "only allocated tasks",
			tasks: []*pod_info.PodInfo{
				simpleTask("task1", "subGroup1", pod_status.Allocated),
				simpleTask("task2", "subGroup1", pod_status.Allocated),
			},
			isRealAllocation: true,
			wantLen:          0,
		},
		{
			name: "releasing and pending tasks",
			tasks: []*pod_info.PodInfo{
				simpleTask("task1", "subGroup1", pod_status.Releasing),
				simpleTask("task2", "subGroup1", pod_status.Pending),
			},
			isRealAllocation:  true,
			wantLen:           1,
			wantFirstTaskName: "task2",
		},
		{
			name: "releasing and pending tasks (virtual allocation)",
			tasks: []*pod_info.PodInfo{
				simpleTask("task1", "subGroup1", pod_status.Releasing),
				simpleTask("task2", "subGroup1", pod_status.Pending),
			},
			isRealAllocation:  false,
			wantLen:           2,
			wantFirstTaskName: "task1",
		},
		{
			name:             "empty queue",
			tasks:            []*pod_info.PodInfo{},
			isRealAllocation: true,
			wantLen:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := subgroup_info.NewPodSet("subGroup1", 1, nil)
			for _, task := range tt.tasks {
				if task.Status == pod_status.Releasing && !tt.isRealAllocation {
					task.IsVirtualStatus = true
				}
				sg.AssignTask(task)
			}
			tasksQueue := getTasksPriorityQueue(sg, tasksOrderFn, tt.isRealAllocation)
			if tasksQueue.Len() != tt.wantLen {
				t.Errorf("want Len=%d, got %d", tt.wantLen, tasksQueue.Len())
			}
			if tt.wantFirstTaskName != "" && tasksQueue.Len() > 0 {
				val := tasksQueue.Pop().(*pod_info.PodInfo)
				if val.Pod.Name != tt.wantFirstTaskName {
					t.Errorf("first task name want=%q, got=%q", tt.wantFirstTaskName, val.Pod.Name)
				}
			}
		})
	}
}

func Test_getNumTasksToAllocate(t *testing.T) {
	tests := []struct {
		name             string
		minAvailable     int
		taskStatuses     []pod_status.PodStatus
		isRealAllocation bool
		want             int
	}{
		{
			name:             "pending equal to minAvailable",
			minAvailable:     3,
			taskStatuses:     []pod_status.PodStatus{pod_status.Pending, pod_status.Pending, pod_status.Pending},
			isRealAllocation: true,
			want:             3, // needs 3, has 0 allocated
		},
		{
			name:             "allocated equal to minAvailable, plus pending",
			minAvailable:     2,
			taskStatuses:     []pod_status.PodStatus{pod_status.Allocated, pod_status.Allocated, pod_status.Pending},
			isRealAllocation: true,
			want:             1, // can allocate at most 1 when minAvailable is reached
		},
		{
			name:             "allocated above minAvailable, extra allocatable pending",
			minAvailable:     2,
			taskStatuses:     []pod_status.PodStatus{pod_status.Allocated, pod_status.Allocated, pod_status.Allocated, pod_status.Pending},
			isRealAllocation: true,
			want:             1, // at most 1 after minAvailable reached
		},
		{
			name:             "allocated less than minAvailable, rest pending",
			minAvailable:     4,
			taskStatuses:     []pod_status.PodStatus{pod_status.Allocated, pod_status.Allocated, pod_status.Pending, pod_status.Pending},
			isRealAllocation: true,
			want:             2, // need 4 - 2 = 2 more
		},
		{
			name:             "all allocated, at minAvailable",
			minAvailable:     3,
			taskStatuses:     []pod_status.PodStatus{pod_status.Allocated, pod_status.Allocated, pod_status.Allocated},
			isRealAllocation: true,
			want:             0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := subgroup_info.NewPodSet("sg", int32(tt.minAvailable), nil)
			for i, status := range tt.taskStatuses {
				task := simpleTask(
					fmt.Sprintf("task-%d", i),
					"sg",
					status,
				)
				if task.Status == pod_status.Releasing && !tt.isRealAllocation {
					task.IsVirtualStatus = true
				}
				sg.AssignTask(task)
			}
			got := getNumTasksToAllocate(sg, tt.isRealAllocation)
			if got != tt.want {
				t.Errorf("getNumTasksToAllocate() = %d, want %d", got, tt.want)
			}
		})
	}
}

func Test_getNumAllocatableTasks(t *testing.T) {
	tests := []struct {
		name             string
		taskStatuses     []pod_status.PodStatus
		isRealAllocation bool
		want             int
	}{
		{
			name:             "no tasks",
			taskStatuses:     nil,
			isRealAllocation: true,
			want:             0,
		},
		{
			name:             "all pending",
			taskStatuses:     []pod_status.PodStatus{pod_status.Pending, pod_status.Pending},
			isRealAllocation: true,
			want:             2,
		},
		{
			name:             "pending and running",
			taskStatuses:     []pod_status.PodStatus{pod_status.Pending, pod_status.Running},
			isRealAllocation: true,
			want:             1, // assuming only Pending is allocatable
		},
		{
			name:             "pending and releasing - real allocation",
			taskStatuses:     []pod_status.PodStatus{pod_status.Pending, pod_status.Releasing},
			isRealAllocation: true,
			want:             1, // only Pending is allocatable with real allocation
		},
		{
			name:             "pending and releasing - non-real allocation",
			taskStatuses:     []pod_status.PodStatus{pod_status.Pending, pod_status.Releasing},
			isRealAllocation: false,
			want:             2, // assuming both Pending and Releasing are allocatable
		},
		{
			name:             "allocated and succeeded",
			taskStatuses:     []pod_status.PodStatus{pod_status.Allocated, pod_status.Succeeded},
			isRealAllocation: true,
			want:             0,
		},
		{
			name:             "all succeeded",
			taskStatuses:     []pod_status.PodStatus{pod_status.Succeeded, pod_status.Succeeded},
			isRealAllocation: true,
			want:             0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := subgroup_info.NewPodSet("test-subgroup", 1, nil)
			for i, status := range tt.taskStatuses {
				p := simpleTask(
					fmt.Sprintf("test-task-%d", i),
					"test-subgroup",
					status,
				)
				p.Pod.UID = types.UID(fmt.Sprintf("test-pod-%d", i))
				if p.Status == pod_status.Releasing && !tt.isRealAllocation {
					p.IsVirtualStatus = true
				}
				sg.AssignTask(p)
			}
			got := getNumAllocatableTasks(sg, tt.isRealAllocation)
			if got != tt.want {
				t.Errorf("getNumAllocatableTasks() = %d, want %d", got, tt.want)
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
				pi := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
				pg.AddTaskInfo(pi)

				if tt.args.overridingStatus != nil {
					pi.Status = tt.args.overridingStatus[i]
				}
			}

			if got := pg.GetActiveAllocatedTasksCount(); got != tt.want {
				t.Errorf("getNumOfAllocatedTasks() = %v, want %v", got, tt.want)
			}
		})
	}
}
