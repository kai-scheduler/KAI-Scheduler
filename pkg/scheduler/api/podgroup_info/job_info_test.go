/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroup_info

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/topology_info"
)

func jobInfoEqual(l, r *PodGroupInfo) bool {
	// Create shallow copies to avoid modifying the original objects
	lCopy := *l
	rCopy := *r

	// Ignore vector fields for comparison (they're tested separately)
	lCopy.AllocatedVector = nil
	rCopy.AllocatedVector = nil
	lCopy.VectorMap = nil
	rCopy.VectorMap = nil

	if !reflect.DeepEqual(lCopy, rCopy) {
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
	case01_task1 := pod_info.NewTaskInfo(case01_pod1, nil, resource_info.NewResourceVectorMap())
	case01_pod2 := common_info.BuildPod(case01_ns, "p2", "n1", v1.PodRunning, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task2 := pod_info.NewTaskInfo(case01_pod2, nil, resource_info.NewResourceVectorMap())
	case01_pod3 := common_info.BuildPod(case01_ns, "p3", "n1", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task3 := pod_info.NewTaskInfo(case01_pod3, nil, resource_info.NewResourceVectorMap())
	case01_pod4 := common_info.BuildPod(case01_ns, "p4", "n1", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), podAnnotations)
	case01_task4 := pod_info.NewTaskInfo(case01_pod4, nil, resource_info.NewResourceVectorMap())

	subGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
	defaultSubGroup := subgroup_info.NewPodSet(DefaultSubGroup, 1, nil).WithPodInfos(pod_info.PodsMap{
		case01_task1.UID: case01_task1,
		case01_task2.UID: case01_task2,
		case01_task3.UID: case01_task3,
		case01_task4.UID: case01_task4,
	})
	subGroupSet.AddPodSet(defaultSubGroup)

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
				UID:             case01_uid,
				Allocated:       common_info.BuildResourceWithPods("4000m", "4G", "3"),
				RootSubGroupSet: subGroupSet,
				PodSets:         map[string]*subgroup_info.PodSet{DefaultSubGroup: defaultSubGroup},
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
				JobFitErrors:         make([]common_info.JobFitError, 0),
				TasksFitErrors:       map[common_info.PodID]*common_info.TasksFitErrors{},
			},
		},
	}

	for i, test := range tests {
		ps := NewPodGroupInfo(test.uid)

		for _, pod := range test.pods {
			pi := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
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
	case01_task1 := pod_info.NewTaskInfo(case01_pod1, nil, resource_info.NewResourceVectorMap())
	case01_pod2 := common_info.BuildPod(case01_ns, "p2", "n1", v1.PodRunning, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), runningPodAnnotations)
	case01_task2 := pod_info.NewTaskInfo(case01_pod2, nil, resource_info.NewResourceVectorMap())
	case01_pod3 := common_info.BuildPod(case01_ns, "p3", "n1", v1.PodRunning, common_info.BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case01_owner}, make(map[string]string), runningPodAnnotations)
	case01_task3 := pod_info.NewTaskInfo(case01_pod3, nil, resource_info.NewResourceVectorMap())
	// case2
	case02_uid := common_info.PodGroupID("owner2")
	case02_ns := "c2"

	case02_owner := common_info.BuildOwnerReference(string(case02_uid))
	case02_pod1 := common_info.BuildPod(case02_ns, "p1", "", v1.PodPending, common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), pendingPodAnnotations)
	case02_task1 := pod_info.NewTaskInfo(case02_pod1, nil, resource_info.NewResourceVectorMap())
	case02_pod2 := common_info.BuildPod(case02_ns, "p2", "n1", v1.PodPending, common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), pendingPodAnnotations)
	case02_task2 := pod_info.NewTaskInfo(case02_pod2, nil, resource_info.NewResourceVectorMap())
	case02_pod3 := common_info.BuildPod(case02_ns, "p3", "n1", v1.PodRunning, common_info.BuildResourceList("3000m", "3G"), []metav1.OwnerReference{case02_owner}, make(map[string]string), runningPodAnnotations)
	case02_task3 := pod_info.NewTaskInfo(case02_pod3, nil, resource_info.NewResourceVectorMap())

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
			expected: func() *PodGroupInfo {
				subGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				defaultSubGroup := subgroup_info.NewPodSet(DefaultSubGroup, 1, nil).
					WithPodInfos(pod_info.PodsMap{
						case01_task1.UID: case01_task1,
						case01_task2.UID: case01_task2,
						case01_task3.UID: case01_task3,
					})
				subGroupSet.AddPodSet(defaultSubGroup)

				return &PodGroupInfo{
					UID:             case01_uid,
					Allocated:       common_info.BuildResourceWithPods("3000m", "3G", "1"),
					RootSubGroupSet: subGroupSet,
					PodSets:         map[string]*subgroup_info.PodSet{DefaultSubGroup: defaultSubGroup},
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Pending: {case01_task1.UID: case01_task1},
						pod_status.Running: {case01_task3.UID: case01_task3},
					},
					activeAllocatedCount: ptr.To(1),
					JobFitErrors:         make([]common_info.JobFitError, 0),
					TasksFitErrors:       map[common_info.PodID]*common_info.TasksFitErrors{},
				}
			}(),
		},
		{
			name:   "add 2 pending owner pod, 1 running owner pod, remove 1 pending owner pod",
			uid:    case02_uid,
			pods:   []*v1.Pod{case02_pod1, case02_pod2, case02_pod3},
			rmPods: []*v1.Pod{case02_pod2},
			expected: func() *PodGroupInfo {
				subGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				defaultSubGroup := subgroup_info.NewPodSet(DefaultSubGroup, 1, nil).
					WithPodInfos(pod_info.PodsMap{
						case02_task1.UID: case02_task1,
						case02_task2.UID: case02_task2,
						case02_task3.UID: case02_task3,
					})
				subGroupSet.AddPodSet(defaultSubGroup)

				return &PodGroupInfo{
					UID:             case02_uid,
					Allocated:       common_info.BuildResourceWithPods("3000m", "3G", "1"),
					RootSubGroupSet: subGroupSet,
					PodSets:         map[string]*subgroup_info.PodSet{DefaultSubGroup: defaultSubGroup},
					PodStatusIndex: map[pod_status.PodStatus]pod_info.PodsMap{
						pod_status.Pending: {
							case02_task1.UID: case02_task1,
						},
						pod_status.Running: {
							case02_task3.UID: case02_task3,
						},
					},
					activeAllocatedCount: ptr.To(1),
					JobFitErrors:         make([]common_info.JobFitError, 0),
					TasksFitErrors:       map[common_info.PodID]*common_info.TasksFitErrors{},
				}
			}(),
		},
	}

	for i, test := range tests {
		ps := NewPodGroupInfo(test.uid)

		for _, pod := range test.pods {
			pi := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
			ps.AddTaskInfo(pi)
		}

		for _, pod := range test.rmPods {
			pi := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
			//nolint:golint,errcheck
			ps.resetTaskState(pi)
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
						}}, nil, resource_info.NewResourceVectorMap())),
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
						}}, nil, resource_info.NewResourceVectorMap())),
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
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}, nil, resource_info.NewResourceVectorMap()),
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
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}, nil, resource_info.NewResourceVectorMap()),
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
					}, nil, resource_info.NewResourceVectorMap()),
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
		minAvailable *int32
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
						}}, nil, resource_info.NewResourceVectorMap())),
			minAvailable: ptr.To(int32(1)),
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
					nil, resource_info.NewResourceVectorMap(),
				),
			),
			minAvailable: ptr.To(int32(1)),
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
						}}, nil, resource_info.NewResourceVectorMap())),
			minAvailable: ptr.To(int32(2)),
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
					nil, resource_info.NewResourceVectorMap(),
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
					nil, resource_info.NewResourceVectorMap(),
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
					nil, resource_info.NewResourceVectorMap(),
				),
			),
			minAvailable: ptr.To(int32(2)),
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
					nil, resource_info.NewResourceVectorMap(),
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
					nil, resource_info.NewResourceVectorMap(),
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
					nil, resource_info.NewResourceVectorMap(),
				),
			),
			minAvailable: ptr.To(int32(3)),
			expected:     false,
		},
		{
			name: "job with subgroups - all ready",
			job: &PodGroupInfo{
				UID: "test-pg",
				PodSets: map[string]*subgroup_info.PodSet{
					"sb-1": subgroup_info.NewPodSet("sb-1", 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"111": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "111",
										Name:      "task1",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
							"222": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "222",
										Name:      "task2",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
					"sb-2": subgroup_info.NewPodSet("sb-2", 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"333": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "333",
										Name:      "task3",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
				},
			},
			expected: true,
		},
		{
			name: "job with subgroups - some already running",
			job: &PodGroupInfo{
				UID: "test-pg",
				PodSets: map[string]*subgroup_info.PodSet{
					"sb-1": subgroup_info.NewPodSet("sb-1", 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"111": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "111",
										Name:      "task1",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
							"222": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "222",
										Name:      "task2",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
					"sb-2": subgroup_info.NewPodSet("sb-2", 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"333": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "333",
										Name:      "task3",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodRunning,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
				},
			},
			expected: true,
		},
		{
			name: "job with subgroups - more then minAvailable",
			job: &PodGroupInfo{
				UID: "test-pg",
				PodSets: map[string]*subgroup_info.PodSet{
					"sb-1": subgroup_info.NewPodSet("sb-1", 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"111": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "111",
										Name:      "task1",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
							"222": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "222",
										Name:      "task2",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
							"333": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "333",
										Name:      "task3",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
					"sb-2": subgroup_info.NewPodSet("sb-2", 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"444": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "444",
										Name:      "task4",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
				},
			},
			expected: true,
		},
		{
			name: "job with subgroups - one is not ready",
			job: &PodGroupInfo{
				UID: "test-pg",
				PodSets: map[string]*subgroup_info.PodSet{
					"sb-1": subgroup_info.NewPodSet("sb-1", 2, nil).
						WithPodInfos(pod_info.PodsMap{
							"111": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "111",
										Name:      "task1",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
					"sb-2": subgroup_info.NewPodSet("sb-2", 1, nil).
						WithPodInfos(pod_info.PodsMap{
							"333": pod_info.NewTaskInfo(
								&v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										UID:       "333",
										Name:      "task3",
										Namespace: "ns1",
									},
									Status: v1.PodStatus{
										Phase: v1.PodPending,
									}},
								nil, resource_info.NewResourceVectorMap(),
							),
						}),
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		if test.minAvailable != nil {
			test.job.GetSubGroups()[DefaultSubGroup].SetMinAvailable(*test.minAvailable)
		}
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
						}}, nil, resource_info.NewResourceVectorMap())),
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
						}}, nil, resource_info.NewResourceVectorMap())),
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
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "333",
							Name:      "task3",
							Namespace: "ns3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						}}, nil, resource_info.NewResourceVectorMap()),
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
						}}, nil, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "222",
							Name:      "task2",
							Namespace: "ns2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						}}, nil, resource_info.NewResourceVectorMap()),
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
					}, nil, resource_info.NewResourceVectorMap()),
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

func TestPodGroupInfo_GetSchedulingConstraintsSignature(t *testing.T) {
	// Helper function to create a pending pod task
	createPendingTask := func(uid string) *pod_info.PodInfo {
		return pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "uid-" + types.UID(uid),
				Name:      uid,
				Namespace: "ns",
			},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}, nil, resource_info.NewResourceVectorMap())
	}

	// Helper function to create a running (allocated) pod task
	createRunningTask := func(uid string) *pod_info.PodInfo {
		return pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       "uid-" + types.UID(uid),
				Name:      uid,
				Namespace: "ns",
			},
			Spec:   v1.PodSpec{NodeName: "node-1"},
			Status: v1.PodStatus{Phase: v1.PodRunning},
		}, nil, resource_info.NewResourceVectorMap())
	}

	tests := []struct {
		name        string
		podGroupA   func() *PodGroupInfo
		podGroupB   func() *PodGroupInfo
		expectEqual bool
	}{
		{
			// PodGroup A:              PodGroup B:
			// root [rack]              root [rack]
			//   └─ pod-1 (pending)       └─ pod-1 (pending)
			name: "flat podgroup with same topology constraint - expects equal",
			podGroupA: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet := subgroup_info.NewPodSet(DefaultSubGroup, 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet := subgroup_info.NewPodSet(DefaultSubGroup, 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: true,
		},
		{
			// PodGroup A:              PodGroup B:
			// root [rack]              root [zone] <--- DIFFERENT
			//   └─ pod-1 (pending)       └─ pod-1 (pending)
			name: "flat podgroup with different topology constraint - expects not equal",
			podGroupA: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet := subgroup_info.NewPodSet(DefaultSubGroup, 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "zone"}) // Different
				podSet := subgroup_info.NewPodSet(DefaultSubGroup, 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                    PodGroup B:
			// root []                        root []
			//   └─ podset-1 [node]             └─ podset-1 [node]
			//        └─ pod-1 (pending)            └─ pod-1 (pending)
			name: "same constraints - expects equal",
			podGroupA: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: true,
		},
		{
			// PodGroup A:                    PodGroup B:
			// root []                        root []
			//   └─ subgroup-alpha [node]       └─ subgroup-beta [node]
			//        └─ pod-1 (pending)             └─ pod-1 (pending)
			// Names differ, but constraints are identical
			name: "different subgroup names with same constraints - expects equal",
			podGroupA: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("subgroup-alpha", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("subgroup-beta", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: true,
		},
		{
			// PodGroup A:                    PodGroup B:
			// root []                        root []
			//   └─ podset-1 [node] <---        └─ podset-1 [rack] <--- DIFFERENT
			//        └─ pod-1 (pending)             └─ pod-1 (pending)
			name: "different bottom PodSet constraints - expects not equal",
			podGroupA: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				topologyConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "rack", // Different from "node"
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, topologyConstraint)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                      PodGroup B:
			// root []                          root []
			//   └─ middle [zone] <---            └─ middle [rack] <--- DIFFERENT
			//        └─ podset-1 []                   └─ podset-1 []
			//             └─ pod-1 (pending)               └─ pod-1 (pending)
			name: "different middle SubGroupSet constraints - expects not equal",
			podGroupA: func() *PodGroupInfo {
				// root -> middle -> podset
				middleConstraint := &topology_info.TopologyConstraintInfo{
					Topology:       "topology",
					PreferredLevel: "zone",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet := subgroup_info.NewSubGroupSet("middle", middleConstraint)
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet.AddPodSet(podSet)
				rootSubGroupSet.AddSubGroup(middleSubGroupSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				// root -> middle -> podset
				middleConstraint := &topology_info.TopologyConstraintInfo{
					Topology:       "topology",
					PreferredLevel: "rack", // Different from "zone"
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet := subgroup_info.NewSubGroupSet("middle", middleConstraint)
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet.AddPodSet(podSet)
				rootSubGroupSet.AddSubGroup(middleSubGroupSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                        PodGroup B:
			// root [node] <---                   root [rack] <--- DIFFERENT
			//   └─ middle []                       └─ middle []
			//        └─ podset-1 []                     └─ podset-1 []
			//             └─ pod-1 (pending)                 └─ pod-1 (pending)
			name: "different top SubGroupSet constraints - expects not equal",
			podGroupA: func() *PodGroupInfo {
				// root (with constraint) -> middle -> podset
				rootConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "node",
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, rootConstraint)
				middleSubGroupSet := subgroup_info.NewSubGroupSet("middle", &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet.AddPodSet(podSet)
				rootSubGroupSet.AddSubGroup(middleSubGroupSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				// root (with different constraint) -> middle -> podset
				rootConstraint := &topology_info.TopologyConstraintInfo{
					Topology:      "topology",
					RequiredLevel: "rack", // Different from "node"
				}
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, rootConstraint)
				middleSubGroupSet := subgroup_info.NewSubGroupSet("middle", &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				middleSubGroupSet.AddPodSet(podSet)
				rootSubGroupSet.AddSubGroup(middleSubGroupSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                      PodGroup B:
			// root []                          root []
			//   └─ podset-1 []                   └─ podset-1 []
			//        ├─ pod-1 (pending)              └─ pod-1 (pending)
			//        └─ pod-2 (pending) <--- EXTRA
			// Extra pending pod affects signature
			name: "extra non-allocated pod - expects not equal",
			podGroupA: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				podSet.AssignTask(createPendingTask("pod-2")) // Extra pending pod
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                        PodGroup B:
			// root []                            root []
			//   └─ podset-1 []                     └─ podset-1 []
			//        ├─ pod-1 (pending)                └─ pod-1 (pending)
			//        └─ pod-2 (running) <--- EXTRA (allocated, ignored in signature)
			// Extra allocated pod does NOT affect signature
			name: "extra allocated pod - expects equal",
			podGroupA: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				podSet.AssignTask(createRunningTask("pod-2")) // Extra allocated pod
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet := subgroup_info.NewPodSet("podset-1", 1, &topology_info.TopologyConstraintInfo{})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: true,
		},
		{
			// PodGroup A:                          PodGroup B:
			// root []                              root []
			//   ├─ podset-1 [rack]                   ├─ podset-1 [rack]
			//   │    └─ pod-1 (pending) <---         │    (empty)
			//   └─ podset-2 [zone]                   └─ podset-2 [zone]
			//        (empty)                              └─ pod-1 (pending) <--- DIFFERENT PODSET
			// Same pod in different PodSets with different topology constraints
			name: "same pod in different podsets - expects not equal",
			podGroupA: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet1 := subgroup_info.NewPodSet("podset-1", 1,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet2 := subgroup_info.NewPodSet("podset-2", 1,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "zone"})
				rootSubGroupSet.AddPodSet(podSet1)
				rootSubGroupSet.AddPodSet(podSet2)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet1.AssignTask(createPendingTask("pod-1")) // Pod in podset-1
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, &topology_info.TopologyConstraintInfo{})
				podSet1 := subgroup_info.NewPodSet("podset-1", 1,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet2 := subgroup_info.NewPodSet("podset-2", 1,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "zone"})
				rootSubGroupSet.AddPodSet(podSet1)
				rootSubGroupSet.AddPodSet(podSet2)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet2.AssignTask(createPendingTask("pod-1")) // Same pod but in podset-2
				return pgi
			},
			expectEqual: false,
		},
		{
			// PodGroup A:                          PodGroup B:
			// root [rack] <---                     root []
			//   └─ podset-1 []                       └─ podset-1 [rack] <---
			//        └─ pod-1 (pending)                   └─ pod-1 (pending)
			// Same constraint value but at different hierarchy levels
			name: "same topology constraint at different hierarchy levels - expects not equal",
			podGroupA: func() *PodGroupInfo {
				// Constraint at root level, empty at podset level
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				podSet := subgroup_info.NewPodSet("podset-1", 1, nil)
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-1",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			podGroupB: func() *PodGroupInfo {
				// Empty at root level, constraint at podset level
				rootSubGroupSet := subgroup_info.NewSubGroupSet(subgroup_info.RootSubGroupSetName, nil)
				podSet := subgroup_info.NewPodSet("podset-1", 1,
					&topology_info.TopologyConstraintInfo{Topology: "topo", RequiredLevel: "rack"})
				rootSubGroupSet.AddPodSet(podSet)

				pgi := &PodGroupInfo{
					UID:             "pg-2",
					RootSubGroupSet: rootSubGroupSet,
					PodSets:         rootSubGroupSet.GetAllPodSets(),
				}
				podSet.AssignTask(createPendingTask("pod-1"))
				return pgi
			},
			expectEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgiA := tt.podGroupA()
			pgiB := tt.podGroupB()

			sigA := pgiA.GetSchedulingConstraintsSignature()
			sigB := pgiB.GetSchedulingConstraintsSignature()

			if tt.expectEqual && sigA != sigB {
				t.Errorf("Expected signatures to be equal, but got sigA=%s, sigB=%s", sigA, sigB)
			}
			if !tt.expectEqual && sigA == sigB {
				t.Errorf("Expected signatures to be different, but both are %s", sigA)
			}
		})
	}
}

func TestPodGroupInfo_IsStale(t *testing.T) {
	tests := []struct {
		name     string
		job      *PodGroupInfo
		expected bool
	}{
		{
			name: "empty PodGroupInfo, not stale",
			job: func() *PodGroupInfo {
				pgi := NewPodGroupInfo("test-podgroup")
				pgi.GetSubGroups()[DefaultSubGroup].SetMinAvailable(1)
				return pgi
			}(),
			expected: false,
		},
		{
			name: "no active used tasks, not stale",
			job: func() *PodGroupInfo {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
					},
					Status: v1.PodStatus{Phase: v1.PodPending},
				}
				task := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
				pgi := NewPodGroupInfo("test-podgroup", task)
				pgi.GetSubGroups()[DefaultSubGroup].SetMinAvailable(1)
				return pgi
			}(),
			expected: false,
		},
		{
			name: "job has succeeded tasks, not stale",
			job: func() *PodGroupInfo {
				pod1 := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
					},
					Status: v1.PodStatus{Phase: v1.PodSucceeded},
				}
				pod2 := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "2",
						Namespace: "ns",
						Name:      "task2",
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}
				task1 := pod_info.NewTaskInfo(pod1, nil, resource_info.NewResourceVectorMap())
				task2 := pod_info.NewTaskInfo(pod2, nil, resource_info.NewResourceVectorMap())
				pgi := NewPodGroupInfo("test-podgroup", task1, task2)
				pgi.GetSubGroups()[DefaultSubGroup].SetMinAvailable(2)
				return pgi
			}(),
			expected: false,
		},
		{
			name: "activeUsedTasks < minAvailable, stale",
			job: func() *PodGroupInfo {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}
				task := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
				pgi := NewPodGroupInfo("test-podgroup", task)
				pgi.GetSubGroups()[DefaultSubGroup].SetMinAvailable(2)
				return pgi
			}(),
			expected: true,
		},
		{
			name: "activeUsedTasks >= minAvailable, no subgroups, not stale",
			job: func() *PodGroupInfo {
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}
				task := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
				pgi := NewPodGroupInfo("test-podgroup", task)
				pgi.GetSubGroups()[DefaultSubGroup].SetMinAvailable(1)
				return pgi
			}(),
			expected: false,
		},
		{
			name: "activeUsedTasks >= minAvailable, subgroups gang NOT satisfied, stale",
			job: func() *PodGroupInfo {
				pgi := NewPodGroupInfo("test-podgroup")

				sg1 := subgroup_info.NewPodSet("sg1", 1, nil)
				pgi.PodSets["sg1"] = sg1

				sg2 := subgroup_info.NewPodSet("sg2", 1, nil)
				pgi.PodSets["sg2"] = sg2

				task1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "sg1",
						},
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}, nil, resource_info.NewResourceVectorMap())

				task2 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "2",
						Namespace: "ns",
						Name:      "task2",
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "sg1",
						},
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}, nil, resource_info.NewResourceVectorMap())

				pgi.AddTaskInfo(task1)
				pgi.AddTaskInfo(task2)

				return pgi
			}(),
			expected: true,
		},
		{
			name: "activeUsedTasks >= minAvailable, subgroups gang satisfied, not stale",
			job: func() *PodGroupInfo {
				pgi := NewPodGroupInfo("test-podgroup")

				sg1 := subgroup_info.NewPodSet("sg1", 1, nil)
				sg2 := subgroup_info.NewPodSet("sg2", 1, nil)
				pgi.PodSets = map[string]*subgroup_info.PodSet{
					"sg1": sg1,
					"sg2": sg2,
				}

				task1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Namespace: "ns",
						Name:      "task1",
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "sg1",
						},
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}, nil, resource_info.NewResourceVectorMap())

				task2 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "2",
						Namespace: "ns",
						Name:      "task2",
						Labels: map[string]string{
							commonconstants.SubGroupLabelKey: "sg2",
						},
					},
					Status: v1.PodStatus{Phase: v1.PodRunning},
				}, nil, resource_info.NewResourceVectorMap())

				pgi.AddTaskInfo(task1)
				pgi.AddTaskInfo(task2)

				return pgi
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		got := tt.job.IsStale()
		if got != tt.expected {
			t.Errorf("IsStale() for case '%s' got %v, want %v", tt.name, got, tt.expected)
		}
	}
}
