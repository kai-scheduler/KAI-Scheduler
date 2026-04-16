// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
)

func jobInfoEqual(l, r *PodGroupInfo) bool {
	if !reflect.DeepEqual(l, r) {
		return false
	}

	return true
}

func TestAddTaskInfo(t *testing.T) {
	// case1
	case01_uid := common_info.PodGroupID("uid")
	case01_ns := "c1"
	case01_owner := common_info.BuildOwnerReference("uid")

	podAnnotations := map[string]string{
		pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
		commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
	}
	case01_pod1 := common_info.BuildPod(case01_ns, "p1", "", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task1 := pod_info.NewTaskInfo(case01_pod1)
	case01_pod2 := common_info.BuildPod(case01_ns, "p2", "n1", v1.PodRunning, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task2 := pod_info.NewTaskInfo(case01_pod2)
	case01_pod3 := common_info.BuildPod(case01_ns, "p3", "n1", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task3 := pod_info.NewTaskInfo(case01_pod3)
	case01_pod4 := common_info.BuildPod(case01_ns, "p4", "n1", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task4 := pod_info.NewTaskInfo(case01_pod4)

	tests := []struct {
		name     string
		uid      common_info.PodGroupID
		pods     []*v1.Pod
		expected *PodGroupInfo
	}{
		{
			name: "add 1 pending owner pod, 1 running owner pod",
			uid:  case01_uid,
			pods: []*v1.Pod{case01_pod1, case01_pod2, case01_pod3, case01_pod4},
			expected: &PodGroupInfo{
				UID:       case01_uid,
				Allocated: common_info.BuildResource("4000m", "4G"),
				PodInfos: pod_info.PodsMap{
					case01_task1.UID: case01_task1,
					case01_task2.UID: case01_task2,
					case01_task3.UID: case01_task3,
					case01_task4.UID: case01_task4,
				},
				PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
					pod_status.Running: {
						case01_task2.UID: case01_task2,
					},
					pod_status.Pending: {
						case01_task1.UID: case01_task1,
					},
					pod_status.Bound: {
						case01_task3.UID: case01_task3,
						case01_task4.UID: case01_task4,
					},
				},
				activeAllocatedCount: ptr.To(3),
				JobFitErrors:         make(v2alpha2.UnschedulableExplanations, 0),
				NodesFitErrors:       map[common_info.PodID]*common_info.FitErrors{},
			},
		},
	}

	for i, test := range tests {
		ps := NewPodGroupInfo(test.uid)

		for _, pod := range test.pods {
			pi := pod_info.NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}

		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}

func TestDeleteTaskInfo(t *testing.T) {
	// case1
	case01_uid := common_info.PodGroupID("owner1")
	case01_ns := "c1"
	runningPodAnnotations := map[string]string{pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular)}
	pendingPodAnnotations := make(map[string]string)

	case01_owner := common_info.BuildOwnerReference(string(case01_uid))
	case01_pod1 := common_info.BuildPod(case01_ns, "p1", "", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), pendingPodAnnotations)
	case01_task1 := pod_info.NewTaskInfo(case01_pod1)
	case01_pod2 := common_info.BuildPod(case01_ns, "p2", "n1", v1.PodRunning, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), runningPodAnnotations)
	case01_task2 := pod_info.NewTaskInfo(case01_pod2)
	case01_pod3 := common_info.BuildPod(case01_ns, "p3", "n1", v1.PodRunning, common_info.BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), runningPodAnnotations)
	case01_task3 := pod_info.NewTaskInfo(case01_pod3)

	case01_task2.Status = pod_status.Deleted
	// case2
	case02_uid := common_info.PodGroupID("owner2")
	case02_ns := "c2"

	case02_owner := common_info.BuildOwnerReference(string(case02_uid))
	case02_pod1 := common_info.BuildPod(case02_ns, "p1", "", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), pendingPodAnnotations)
	case02_task1 := pod_info.NewTaskInfo(case02_pod1)
	case02_pod2 := common_info.BuildPod(case02_ns, "p2", "n1", v1.PodPending, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), pendingPodAnnotations)
	case02_task2 := pod_info.NewTaskInfo(case02_pod2)
	case02_pod3 := common_info.BuildPod(case02_ns, "p3", "n1", v1.PodRunning, common_info.BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), runningPodAnnotations)
	case02_task3 := pod_info.NewTaskInfo(case02_pod3)

	case02_task2.Status = pod_status.Deleted

	tests := []struct {
		name     string
		uid      common_info.PodGroupID
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *PodGroupInfo
	}{
		{
			name:   "add 1 pending owner pod, 2 running owner pod, remove 1 running owner pod",
			uid:    case01_uid,
			pods:   []*v1.Pod{case01_pod1, case01_pod2, case01_pod3},
			rmPods: []*v1.Pod{case01_pod2},
			expected: &PodGroupInfo{
				UID:       case01_uid,
				Allocated: common_info.BuildResource("3000m", "3G"),
				PodInfos: pod_info.PodsMap{
					case01_task1.UID: case01_task1,
					case01_task2.UID: case01_task2,
					case01_task3.UID: case01_task3,
				},
				PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
					pod_status.Pending: {case01_task1.UID: case01_task1},
					pod_status.Running: {case01_task3.UID: case01_task3},
				},
				activeAllocatedCount: ptr.To(1),
				JobFitErrors:         make(v2alpha2.UnschedulableExplanations, 0),
				NodesFitErrors:       map[common_info.PodID]*common_info.FitErrors{},
			},
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02_uid,
			pods:   []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods: []*v1.Pod{case02_pod2},
			expected: &PodGroupInfo{
				UID:       case02_uid,
				Allocated: common_info.BuildResource("3000m", "3G"),
				PodInfos: pod_info.PodsMap{
					case02_task1.UID: case02_task1,
					case02_task2.UID: case02_task2,
					case02_task3.UID: case02_task3,
				},
				PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
					pod_status.Pending: {
						case02_task1.UID: case02_task1,
					},
					pod_status.Running: {
						case02_task3.UID: case02_task3,
					},
				},
				activeAllocatedCount: ptr.To(1),
				JobFitErrors:         make(v2alpha2.UnschedulableExplanations, 0),
				NodesFitErrors:       map[common_info.PodID]*common_info.FitErrors{},
			},
		},
	}

	for i, test := range tests {
		ps := NewPodGroupInfo(test.uid)

		for _, pod := range test.pods {
			pi := pod_info.NewTaskInfo(pod)
			ps.AddTaskInfo(pi)
		}

		for _, pod := range test.rmPods {
			pi := pod_info.NewTaskInfo(pod)
			//nolint:golint,errcheck
			ps.DeleteTaskInfo(pi)
		}

		if !jobInfoEqual(ps, test.expected) {
			t.Errorf("podset info %d: \n expected: %v, \n got: %v \n",
				i, test.expected, ps)
		}
	}
}

func TestPodGroupInfo_GetNumAliveTasks(t *testing.T) {
	tests := []struct {
		name     string
		job      *PodGroupInfo
		expected int
	}{
		{
			name:     "job without tasks",
			job:      NewPodGroupInfo("123"),
			expected: 0,
		},
		{
			name: "job with single pending task",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}})),
			expected: 1,
		},
		{
			name: "job with single completed task",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodSucceeded,
						}})),
			expected: 0,
		},
		{
			name: "job with multiple tasks",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}),
			),
			expected: 2,
		},
		{
			name: "job with multiple tasks, some gated",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "444",
							Name:      "task4",
							Namespace: "ns4",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
						Spec: v1.PodSpec{
							SchedulingGates: []v1.PodSchedulingGate{
								{
									Name: "gated",
								},
							},
						},
					}),
			),
			expected: 3,
		},
	}

	for _, test := range tests {
		result := test.job.GetNumAliveTasks()
		if test.expected != result {
			t.Errorf("GetNumAliveTasks failed. test '%s'. expected: %v, actual: %v",
				test.name, test.expected, result)
		}
	}
}

func TestPodGroupInfo_IsReadyForScheduling(t *testing.T) {
	tests := []struct {
		name         string
		job          *PodGroupInfo
		minAvailable int32
		expected     bool
	}{
		{
			name: "job with pending task",
			job: NewPodGroupInfo("test-pg",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}})),
			minAvailable: 1,
			expected:     true,
		},
		{
			name: "job with gated task",
			job: NewPodGroupInfo("test-pg",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
						Spec: v1.PodSpec{
							SchedulingGates: []v1.PodSchedulingGate{
								{
									Name: "gated",
								},
							},
						},
					},
				),
			),
			minAvailable: 1,
			expected:     false,
		},
		{
			name: "job with pending task, minAvailable 2",
			job: NewPodGroupInfo("test-pg",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}})),
			minAvailable: 2,
			expected:     false,
		},
		{
			name: "job with pending & gated tasks",
			job: NewPodGroupInfo("test-pg",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns1",
						},
						Spec: v1.PodSpec{
							SchedulingGates: []v1.PodSchedulingGate{
								{
									Name: "gated",
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
			),
			minAvailable: 2,
			expected:     true,
		},
		{
			name: "job with pending & gated tasks",
			job: NewPodGroupInfo("test-pg",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns1",
						},
						Spec: v1.PodSpec{
							SchedulingGates: []v1.PodSchedulingGate{
								{
									Name: "gated",
								},
							},
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}},
				),
			),
			minAvailable: 3,
			expected:     false,
		},
	}

	for _, test := range tests {
		test.job.MinAvailable = test.minAvailable
		result := test.job.IsReadyForScheduling()
		if result != test.expected {
			t.Errorf("IsReadyForScheduling failed. test '%s'. expected: %v, actual: %v",
				test.name, test.expected, result)
		}
	}
}

func TestPodGroupInfo_GetNumPendingTasks(t *testing.T) {
	tests := []struct {
		name     string
		job      *PodGroupInfo
		expected int
	}{
		{
			name:     "job without tasks",
			job:      NewPodGroupInfo("123"),
			expected: 0,
		},
		{
			name: "job with single pending task",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}})),
			expected: 1,
		},
		{
			name: "job with single running task",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}})),
			expected: 0,
		},
		{
			name: "job with multiple tasks",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}),
			),
			expected: 1,
		},
		{
			name: "job with multiple tasks, some gated",
			job: NewPodGroupInfo("123",
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "111",
							Name:      "task1",
							Namespace: "ns1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						},
						Spec: v1.PodSpec{
							SchedulingGates: []v1.PodSchedulingGate{
								{
									Name: "gated",
								},
							},
						},
					}),
			),
			expected: 1,
		},
	}

	for _, test := range tests {
		result := test.job.GetNumPendingTasks()
		if test.expected != result {
			t.Errorf("GetNumPendingTasks failed. test '%s'. expected: %v, actual: %v",
				test.name, test.expected, result)
		}
	}
}
