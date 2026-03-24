// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package accumulated_scenario_filters

import (
	"cmp"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/actions/common/solvers/scenario"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/framework"
)

func Test_orderedInsert(t *testing.T) {
	type args[T cmp.Ordered] struct {
		array   []T
		value   T
		replace bool
		cmp     func(T, T) int
	}
	type testCase[T cmp.Ordered] struct {
		name string
		args args[T]
		want []T
	}
	tests := []testCase[string]{
		{
			name: "empty array",
			args: args[string]{
				array:   []string{},
				value:   "1",
				replace: false,
				cmp:     cmp.Compare[string],
			},
			want: []string{"1"},
		},
		{
			name: "add at the beginning of the array",
			args: args[string]{
				array:   []string{"1"},
				value:   "0",
				replace: false,
				cmp:     cmp.Compare[string],
			},
			want: []string{"0", "1"},
		},
		{
			name: "replace in array",
			args: args[string]{
				array:   []string{"1"},
				value:   "0",
				replace: true,
				cmp:     cmp.Compare[string],
			},
			want: []string{"0"},
		},
		{
			name: "update element in array",
			args: args[string]{
				array:   []string{"b", "a", "c"},
				value:   "a",
				replace: true,
				cmp: func(s string, s2 string) int {
					// A comparison func that would want to sort "a", "b", "c"
					if s == s2 {
						return 0
					}
					if s == "c" {
						return 1
					}
					if s2 == "c" {
						return -1
					}
					if s == "a" && s2 == "b" {
						return -1
					}
					return 1
				},
			},
			want: []string{"a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orderedInsert(tt.args.array, tt.args.value, tt.args.replace, tt.args.cmp); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("orderedInsert() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccumulatedIdleGpus_matchRelevantNodeToTask(t *testing.T) {
	type fields struct {
		nodesNameToIdleGpus   map[string]float64
		maxFreeGpuNodesSorted []string
	}
	type args struct {
		pendingTaskGpus  float64
		filterMatchState matchingState
	}
	type want struct {
		canAllocate                   bool
		nodesToVirtuallyAllocatedGpus map[string]float64
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   want
	}{
		{
			name: "match to free node",
			fields: fields{
				nodesNameToIdleGpus: map[string]float64{
					"n1": 1.0,
				},
				maxFreeGpuNodesSorted: []string{"n1"},
			},
			args: args{
				pendingTaskGpus: 0.5,
				filterMatchState: matchingState{
					nodesToVirtuallyAllocatedGpus: map[string]float64{},
				},
			},
			want: want{
				canAllocate:                   true,
				nodesToVirtuallyAllocatedGpus: map[string]float64{"n1": 0.5},
			},
		},
		{
			name: "cannot match to node with no gpus",
			fields: fields{
				nodesNameToIdleGpus: map[string]float64{
					"n1": 0.0,
				},
				maxFreeGpuNodesSorted: []string{"n1"},
			},
			args: args{
				pendingTaskGpus: 0.5,
				filterMatchState: matchingState{
					nodesToVirtuallyAllocatedGpus: map[string]float64{},
				},
			},
			want: want{
				canAllocate:                   false,
				nodesToVirtuallyAllocatedGpus: map[string]float64{},
			},
		},
		{
			name: "cannot match to node full with virtual allocations",
			fields: fields{
				nodesNameToIdleGpus: map[string]float64{
					"n1": 1.0,
				},
				maxFreeGpuNodesSorted: []string{"n1"},
			},
			args: args{
				pendingTaskGpus: 0.5,
				filterMatchState: matchingState{
					nodesToVirtuallyAllocatedGpus: map[string]float64{"n1": 1.0},
				},
			},
			want: want{
				canAllocate:                   false,
				nodesToVirtuallyAllocatedGpus: map[string]float64{"n1": 1.0},
			},
		},
		{
			name: "match the second node",
			fields: fields{
				nodesNameToIdleGpus: map[string]float64{
					"n1": 1.0,
					"n2": 2.0,
				},
				maxFreeGpuNodesSorted: []string{"n2", "n1"},
			},
			args: args{
				pendingTaskGpus: 0.5,
				filterMatchState: matchingState{
					nodesToVirtuallyAllocatedGpus: map[string]float64{"n1": 0.5},
				},
			},
			want: want{
				canAllocate:                   true,
				nodesToVirtuallyAllocatedGpus: map[string]float64{"n1": 0.5, "n2": 0.5},
			},
		},
		{
			name: "append to nodesToVirtuallyAllocatedGpus",
			fields: fields{
				nodesNameToIdleGpus: map[string]float64{
					"n1": 1.0,
					"n2": 2.0,
				},
				maxFreeGpuNodesSorted: []string{"n2", "n1"},
			},
			args: args{
				pendingTaskGpus: 0.5,
				filterMatchState: matchingState{
					nodesToVirtuallyAllocatedGpus: map[string]float64{"n2": 0.5},
				},
			},
			want: want{
				canAllocate:                   true,
				nodesToVirtuallyAllocatedGpus: map[string]float64{"n2": 1.0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ig := &AccumulatedIdleGpus{
				nodesNameToIdleGpus:   tt.fields.nodesNameToIdleGpus,
				maxFreeGpuNodesSorted: tt.fields.maxFreeGpuNodesSorted,
			}
			got := ig.matchRelevantNodeToTask(tt.args.pendingTaskGpus, tt.args.filterMatchState)
			if got != tt.want.canAllocate {
				t.Errorf("matchRelevantNodeToTask() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(
				tt.args.filterMatchState.nodesToVirtuallyAllocatedGpus, tt.want.nodesToVirtuallyAllocatedGpus) {
				t.Errorf("matchRelevantNodeToTask().nodesToVirtuallyAllocatedGpus = %v, want %v",
					tt.args.filterMatchState.nodesToVirtuallyAllocatedGpus,
					tt.want.nodesToVirtuallyAllocatedGpus)
			}
		})
	}
}

func TestAccumulatedIdleGpus_updateWithVictim(t *testing.T) {
	type fields struct {
		nodesNameToIdleGpus   map[string]float64
		maxFreeGpuNodesSorted []string
	}
	type args struct {
		victimTask          *pod_info.PodInfo
		minIdleGpusRelevant string
		relevantCacheData   map[common_info.PodID]bool
	}
	type want struct {
		minIdleGpusRelevant   string
		relevantCacheData     map[common_info.PodID]bool
		maxFreeGpuNodesSorted []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   want
	}{
		{
			name: "new victim update",
			fields: fields{
				map[string]float64{"n1": 1.0, "n2": 2.0},
				[]string{"n2"},
			},
			args: args{
				victimTask: &pod_info.PodInfo{
					NodeName: "n1",
					UID:      "uid1",
					AcceptedResource: &resource_info.ResourceRequirements{
						GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
							2, 0,
						),
					},
				},
				minIdleGpusRelevant: "n2",
				relevantCacheData:   map[common_info.PodID]bool{},
			},
			want: want{
				minIdleGpusRelevant:   "n1",
				relevantCacheData:     map[common_info.PodID]bool{"uid1": true},
				maxFreeGpuNodesSorted: []string{"n1"},
			},
		},
		{
			name: "do not update maxFreeGpu nodes",
			fields: fields{
				map[string]float64{"n1": 1.0, "n2": 5.0},
				[]string{"n2"},
			},
			args: args{
				victimTask: &pod_info.PodInfo{
					NodeName: "n1",
					UID:      "uid1",
					AcceptedResource: &resource_info.ResourceRequirements{
						GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
							2, 0,
						),
					},
				},
				minIdleGpusRelevant: "n2",
				relevantCacheData:   map[common_info.PodID]bool{},
			},
			want: want{
				minIdleGpusRelevant:   "n2",
				relevantCacheData:     map[common_info.PodID]bool{"uid1": true},
				maxFreeGpuNodesSorted: []string{"n2"},
			},
		},
		{
			name: "new victim - middle of max free gpu list",
			fields: fields{
				map[string]float64{"n1": 1.0, "n2": 2.0, "n3": 5.0, "n4": 2.0},
				[]string{"n3", "n2", "n4"},
			},
			args: args{
				victimTask: &pod_info.PodInfo{
					NodeName: "n1",
					UID:      "uid1",
					AcceptedResource: &resource_info.ResourceRequirements{
						GpuResourceRequirement: *resource_info.NewGpuResourceRequirementWithGpus(
							2, 0,
						),
					},
				},
				minIdleGpusRelevant: "n4",
				relevantCacheData:   map[common_info.PodID]bool{},
			},
			want: want{
				minIdleGpusRelevant:   "n2",
				relevantCacheData:     map[common_info.PodID]bool{"uid1": true},
				maxFreeGpuNodesSorted: []string{"n3", "n1", "n2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ig := &AccumulatedIdleGpus{
				nodesNameToIdleGpus:   tt.fields.nodesNameToIdleGpus,
				maxFreeGpuNodesSorted: tt.fields.maxFreeGpuNodesSorted,
			}
			if got := ig.updateWithVictim(tt.args.victimTask, tt.args.minIdleGpusRelevant, tt.args.relevantCacheData); got != tt.want.minIdleGpusRelevant {
				t.Errorf("updateWithVictim() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.args.relevantCacheData, tt.want.relevantCacheData) {
				t.Errorf("updateWithVictim().relevantCacheData = %v, want %v",
					tt.args.relevantCacheData, tt.want.relevantCacheData)
			}
			if !reflect.DeepEqual(ig.maxFreeGpuNodesSorted, tt.want.maxFreeGpuNodesSorted) {
				t.Errorf("updateWithVictim().maxFreeGpuNodesSorted = %v, want %v",
					ig.maxFreeGpuNodesSorted, tt.want.maxFreeGpuNodesSorted)
			}
		})
	}
}

func TestAccumulatedIdleGpus_updateStateWithScenario(t *testing.T) {
	type fields struct {
		requiredGpusSorted      []float64
		nodesNameToIdleGpus     map[string]float64
		maxFreeGpuNodesSorted   []string
		pendingTasksInState     map[common_info.PodID]bool
		recordedVictimsInCache  map[common_info.PodID]bool
		potentialVictimsInCache map[common_info.PodID]bool
	}
	type args struct {
		scenario        *scenario.ByNodeScenario
		isFirstScenario bool
	}
	type want struct {
		wantErr           bool
		expectedErrorData string

		nodesNameToIdleGpus     map[string]float64
		maxFreeGpuNodesSorted   []string
		pendingTasksInState     map[common_info.PodID]bool
		recordedVictimsInCache  map[common_info.PodID]bool
		potentialVictimsInCache map[common_info.PodID]bool
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   want
	}{
		{
			name: "no nodes available",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{},
				maxFreeGpuNodesSorted:   []string{},
				pendingTasksInState:     map[common_info.PodID]bool{},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(nil,
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{},
				),
				isFirstScenario: true,
			},
			want: want{},
		},
		{
			name: "pending tasks only - first scenario",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(nil,
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{},
				),
				isFirstScenario: true,
			},
			want: want{
				wantErr:               false,
				nodesNameToIdleGpus:   map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted: []string{"n2"},
				pendingTasksInState:   map[common_info.PodID]bool{"uid1": true},
			},
		},
		{
			name: "pending tasks only - assert failed",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(nil,
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{},
				),
				isFirstScenario: false,
			},
			want: want{
				wantErr:           true,
				expectedErrorData: "accumulatedIdleGpus requires all the filters scenarios using the same instance to be based on the same scenario with accumulation of potential victims. The pending task uid1 didn't appear in the scenario given at the AccumulatedIdleGpus ctor",
			},
		},
		{
			name: "pending tasks only - non first scenario",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(nil,
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{},
				),
				isFirstScenario: true,
			},
			want: want{
				wantErr:               false,
				nodesNameToIdleGpus:   map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted: []string{"n2"},
				pendingTasksInState:   map[common_info.PodID]bool{"uid1": true},
			},
		},
		{
			name: "potential victims scenario",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
					},
				}, nodePodAffinityInfo)

				potentialVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(potentialVictim1)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"pv1pg": podgroup_info.NewPodGroupInfo("pv1pg"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								UID:       "uid1",
								Name:      "pending1",
								Namespace: "n1",
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						})),
						[]*pod_info.PodInfo{potentialVictim1},
						[]*podgroup_info.PodGroupInfo{},
					),
					isFirstScenario: false,
				}
			}(),
			want: want{
				wantErr:                 false,
				nodesNameToIdleGpus:     map[string]float64{"n1": 4.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n1"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				potentialVictimsInCache: map[common_info.PodID]bool{"uid2": true},
			},
		},
		{
			name: "potential victims scenario - assert failed",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{"uid4": true},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
					},
				}, nodePodAffinityInfo)

				potentialVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(potentialVictim1)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"pv1pg": podgroup_info.NewPodGroupInfo("pv1pg"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								UID:       "uid1",
								Name:      "pending1",
								Namespace: "n1",
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						})),
						[]*pod_info.PodInfo{potentialVictim1},
						[]*podgroup_info.PodGroupInfo{},
					),
					isFirstScenario: false,
				}
			}(),
			want: want{
				wantErr:           true,
				expectedErrorData: "accumulatedIdleGpus requires all the filters scenarios using the same instance to be based on the same scenario with accumulation of potential victims. The list of potential victims for the current scenario should contain the previous list of potential victims. Only 0 out 1 tasks are the contained in the current scenario",
			},
		},
		{
			name: "recorded victims scenario - first scenario",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
					},
				}, nodePodAffinityInfo)

				recordedVictim := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "rv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(recordedVictim)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"rv1pg": podgroup_info.NewPodGroupInfo("rv1pg"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								UID:       "uid1",
								Name:      "pending1",
								Namespace: "n1",
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						})),
						[]*pod_info.PodInfo{},
						[]*podgroup_info.PodGroupInfo{podgroup_info.NewPodGroupInfo("rv1pg", recordedVictim)},
					),
					isFirstScenario: true,
				}
			}(),
			want: want{
				wantErr:                false,
				nodesNameToIdleGpus:    map[string]float64{"n1": 4.0, "n2": 2.0},
				maxFreeGpuNodesSorted:  []string{"n1"},
				pendingTasksInState:    map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache: map[common_info.PodID]bool{"uid2": true},
			},
		},
		{
			name: "recorded victims scenario - non first scenario - do not update",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 4.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n1"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{"uid2": true},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(&framework.Session{
					PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"rv1pg": podgroup_info.NewPodGroupInfo("rv1pg"),
					}},
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{podgroup_info.NewPodGroupInfo("rv1pg", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid2",
							Name:      "pv1",
							Namespace: "n2",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "rv1pg",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "n1",
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("3"),
										},
									},
								},
							},
						},
					}))},
				),
				isFirstScenario: false,
			},
			want: want{
				wantErr:                false,
				nodesNameToIdleGpus:    map[string]float64{"n1": 4.0, "n2": 2.0},
				maxFreeGpuNodesSorted:  []string{"n1"},
				pendingTasksInState:    map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache: map[common_info.PodID]bool{"uid2": true},
			},
		},
		{
			name: "recorded victims scenario - assert failed",
			fields: fields{
				nodesNameToIdleGpus:     map[string]float64{"n1": 1.0, "n2": 2.0},
				maxFreeGpuNodesSorted:   []string{"n2"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: args{
				scenario: scenario.NewByNodeScenario(&framework.Session{
					PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"rv1pg": podgroup_info.NewPodGroupInfo("rv1pg"),
					}},
					nil,
					podgroup_info.NewPodGroupInfo("pendingPg1", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid1",
							Name:      "pending1",
							Namespace: "n1",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("1"),
										},
									},
								},
							},
						},
					})),
					[]*pod_info.PodInfo{},
					[]*podgroup_info.PodGroupInfo{podgroup_info.NewPodGroupInfo("rv1pg", pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							UID:       "uid2",
							Name:      "pv1",
							Namespace: "n2",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "rv1pg",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "n1",
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"nvidia.com/gpu": resource.MustParse("3"),
										},
									},
								},
							},
						},
					}))},
				),
				isFirstScenario: false,
			},
			want: want{
				wantErr:           true,
				expectedErrorData: "accumulatedIdleGpus requires all the filters scenarios using the same instance to be based on the same scenario with accumulation of potential victims. The recorded victims should remain the same between the different scenario filtering. 0 cache hits, pre update recorded tasks seen 0, post update recorded tasks seen 1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ig := &AccumulatedIdleGpus{
				requiredGpusSorted:      tt.fields.requiredGpusSorted,
				nodesNameToIdleGpus:     tt.fields.nodesNameToIdleGpus,
				maxFreeGpuNodesSorted:   tt.fields.maxFreeGpuNodesSorted,
				pendingTasksInState:     tt.fields.pendingTasksInState,
				recordedVictimsInCache:  tt.fields.recordedVictimsInCache,
				potentialVictimsInCache: tt.fields.potentialVictimsInCache,
			}
			err := ig.updateStateWithScenario(tt.args.scenario, tt.args.isFirstScenario)
			if err != nil != tt.want.wantErr {
				t.Errorf("updateStateWithScenario() error = %v, err %v", err, tt.want.wantErr)
			}
			if tt.want.wantErr && err != nil {
				if err.Error() != tt.want.expectedErrorData {
					t.Errorf("updateStateWithScenario().expectedErrorData error = %v, err %v", err.Error(),
						tt.want.expectedErrorData)
				}
			}
			if tt.want.nodesNameToIdleGpus != nil {
				if !reflect.DeepEqual(ig.nodesNameToIdleGpus, tt.want.nodesNameToIdleGpus) {
					t.Errorf("updateStateWithScenario().nodesNameToIdleGpus = %v, want %v",
						ig.nodesNameToIdleGpus, tt.want.nodesNameToIdleGpus)
				}
			}
			if tt.want.maxFreeGpuNodesSorted != nil {
				if !reflect.DeepEqual(ig.maxFreeGpuNodesSorted, tt.want.maxFreeGpuNodesSorted) {
					t.Errorf("updateStateWithScenario().maxFreeGpuNodesSorted = %v, want %v",
						ig.maxFreeGpuNodesSorted, tt.want.maxFreeGpuNodesSorted)
				}
			}
			if tt.want.pendingTasksInState != nil {
				if !reflect.DeepEqual(ig.pendingTasksInState, tt.want.pendingTasksInState) {
					t.Errorf("updateStateWithScenario().pendingTasksInState = %v, want %v",
						ig.pendingTasksInState, tt.want.pendingTasksInState)
				}
			}
			if tt.want.recordedVictimsInCache != nil {
				if !reflect.DeepEqual(ig.recordedVictimsInCache, tt.want.recordedVictimsInCache) {
					t.Errorf("updateStateWithScenario().recordedVictimsInCache = %v, want %v",
						ig.recordedVictimsInCache, tt.want.recordedVictimsInCache)
				}
			}
			if tt.want.pendingTasksInState != nil {
				if !reflect.DeepEqual(ig.pendingTasksInState, tt.want.pendingTasksInState) {
					t.Errorf("updateStateWithScenario().pendingTasksInState = %v, want %v",
						ig.pendingTasksInState, tt.want.pendingTasksInState)
				}
			}
		})
	}
}

func TestAccumulatedIdleGpus_Filter(t *testing.T) {
	type fields struct {
		requiredGpusSorted      []float64
		nodesNameToIdleGpus     map[string]float64
		maxFreeGpuNodesSorted   []string
		pendingTasksInState     map[common_info.PodID]bool
		recordedVictimsInCache  map[common_info.PodID]bool
		potentialVictimsInCache map[common_info.PodID]bool
	}
	type args struct {
		scenario *scenario.ByNodeScenario
	}
	type want struct {
		validScenario bool
		err           bool
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   want
	}{
		{
			name: "exact amount of GPUs after removing potential victim - valid scenario",
			fields: fields{
				requiredGpusSorted:      []float64{10, 8, 2},
				nodesNameToIdleGpus:     map[string]float64{"n1": 9.0, "n2": 8.0},
				maxFreeGpuNodesSorted:   []string{"n2", "n1"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true, "uid5": true, "uid6": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{"uid3": true},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
					},
				}, nodePodAffinityInfo)

				potentialVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				recordedVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid3",
						Name:      "rv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "rv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(potentialVictim1)
				node1.AddTask(recordedVictim1)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"rv1pg": podgroup_info.NewPodGroupInfo("rv1pg"),
							"pv1pg": podgroup_info.NewPodGroupInfo("pv1pg"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid1",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("10"),
												},
											},
										},
									},
								},
							}),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid5",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("8"),
												},
											},
										},
									},
								},
							}),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid6",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("2"),
												},
											},
										},
									},
								},
							}),
						),
						[]*pod_info.PodInfo{
							potentialVictim1,
						},
						[]*podgroup_info.PodGroupInfo{podgroup_info.NewPodGroupInfo("rv1pg", recordedVictim1)},
					),
				}
			}(),
			want: want{
				validScenario: true,
				err:           false,
			},
		},
		{
			name: "insufficient amount of GPUs for largest job - invalid scenario",
			fields: fields{
				requiredGpusSorted:      []float64{10, 8, 2},
				nodesNameToIdleGpus:     map[string]float64{"n1": 6.0, "n2": 8.0},
				maxFreeGpuNodesSorted:   []string{"n2", "n1"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true, "uid5": true, "uid6": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{"uid3": true},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
					},
				}, nodePodAffinityInfo)

				potentialVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				recordedVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid3",
						Name:      "rv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "rv1pg",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("3"),
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(potentialVictim1)
				node1.AddTask(recordedVictim1)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"rv1pg": podgroup_info.NewPodGroupInfo("rv1pg"),
							"pv1pg": podgroup_info.NewPodGroupInfo("pv1pg"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid1",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("10"),
												},
											},
										},
									},
								},
							}),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid5",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("8"),
												},
											},
										},
									},
								},
							}),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid6",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("2"),
												},
											},
										},
									},
								},
							}),
						),
						[]*pod_info.PodInfo{
							potentialVictim1,
						},
						[]*podgroup_info.PodGroupInfo{podgroup_info.NewPodGroupInfo("rv1pg", recordedVictim1)},
					),
				}
			}(),
			want: want{
				validScenario: false,
				err:           false,
			},
		},
		{
			name: "gpu memory pods counted for freed resources - scenario valid",
			fields: fields{
				requiredGpusSorted:      []float64{1},
				nodesNameToIdleGpus:     map[string]float64{"n1": 0.8},
				maxFreeGpuNodesSorted:   []string{"n1"},
				pendingTasksInState:     map[common_info.PodID]bool{"uid1": true},
				recordedVictimsInCache:  map[common_info.PodID]bool{},
				potentialVictimsInCache: map[common_info.PodID]bool{},
			},
			args: func() args {
				controller := gomock.NewController(t)
				nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
				nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()
				nodePodAffinityInfo.EXPECT().RemovePod(gomock.Any()).AnyTimes()

				node1 := node_info.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "n1",
						Labels: map[string]string{
							commonconstants.NvidiaGpuMemory: "100",
						},
					},
				}, nodePodAffinityInfo)

				potentialVictim1 := pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "uid2",
						Name:      "pv1",
						Namespace: "n2",
						Annotations: map[string]string{
							commonconstants.GpuMemory:                "20",
							commonconstants.PodGroupAnnotationForPod: "potential_victims",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "n1",
						Containers: []v1.Container{
							{},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				})

				node1.AddTask(potentialVictim1)

				return args{
					scenario: scenario.NewByNodeScenario(&framework.Session{
						PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
							"potential_victims": podgroup_info.NewPodGroupInfo("potential_victims"),
						}},
						nil,
						podgroup_info.NewPodGroupInfo("pendingPodGroup",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									UID:       "uid1",
									Name:      "pending1",
									Namespace: "n1",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Resources: v1.ResourceRequirements{
												Requests: v1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("1"),
												},
											},
										},
									},
								},
							}),
						),
						[]*pod_info.PodInfo{potentialVictim1},
						[]*podgroup_info.PodGroupInfo{},
					),
				}
			}(),
			want: want{
				validScenario: true,
				err:           false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ig := &AccumulatedIdleGpus{
				requiredGpusSorted:      tt.fields.requiredGpusSorted,
				nodesNameToIdleGpus:     tt.fields.nodesNameToIdleGpus,
				maxFreeGpuNodesSorted:   tt.fields.maxFreeGpuNodesSorted,
				pendingTasksInState:     tt.fields.pendingTasksInState,
				recordedVictimsInCache:  tt.fields.recordedVictimsInCache,
				potentialVictimsInCache: tt.fields.potentialVictimsInCache,
			}
			validScenario, err := ig.Filter(tt.args.scenario)
			if (err != nil) != tt.want.err {
				t.Errorf("Filter() error = %v, err %v", err, tt.want.err)
				return
			}
			if validScenario != tt.want.validScenario {
				t.Errorf("Filter().filtered got = %v, want %v", validScenario, tt.want.validScenario)
			}
		})
	}
}
