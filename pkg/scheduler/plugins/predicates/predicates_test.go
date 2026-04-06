// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/predicates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/plugins_fake/predicates_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func Test_evaluateTaskOnPrePredicate(t *testing.T) {
	type args struct {
		task          *pod_info.PodInfo
		k8sPredicates k8s_internal.SessionPredicates
	}
	tests := []struct {
		name            string
		args            args
		wantErr         bool
		wantedErrorData []string
	}{
		{
			"All pre-predicates empty",
			args{
				task: &pod_info.PodInfo{
					Name:      "p1",
					Namespace: "ns1",
					Pod:       &v1.Pod{},
				},
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			false,
			[]string{},
		},
		{
			name: "One failing pre predicate",
			args: args{
				task: &pod_info.PodInfo{
					Name:      "p1",
					Namespace: "ns1",
					Pod:       &v1.Pod{},
				},
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.FailingPrePredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			wantErr: true,
			wantedErrorData: []string{"Scheduling conditions were not met for pod ns1/p1:",
				"PodFitsHostPorts: failed pre-predicate PodFitsHostPorts. Reasons: reason1, reason2"},
		},
		{
			name: "One failing pre predicate - error the same s reason",
			args: args{
				task: &pod_info.PodInfo{
					Name:      "p1",
					Namespace: "ns1",
					Pod:       &v1.Pod{},
				},
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts: predicates_fake.FailingPrePredicateReasonSameAsError(
						predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			wantErr: true,
			wantedErrorData: []string{"Scheduling conditions were not met for pod ns1/p1:",
				"PodFitsHostPorts: failed pre-predicate PodFitsHostPorts."},
		},
		{
			name: "One failing pre predicate - no reasons",
			args: args{
				task: &pod_info.PodInfo{
					Name:      "p1",
					Namespace: "ns1",
					Pod:       &v1.Pod{},
				},
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.FailingPrePredicateNoReason(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			wantErr: true,
			wantedErrorData: []string{"Scheduling conditions were not met for pod ns1/p1:",
				"PodFitsHostPorts: failed pre-predicate PodFitsHostPorts."},
		},
		{
			name: "Multiple failing pre predicate",
			args: args{
				task: &pod_info.PodInfo{
					Name:      "p1",
					Namespace: "ns1",
					Pod:       &v1.Pod{},
				},
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.FailingPrePredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.FailingPrePredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			wantErr: true,
			wantedErrorData: []string{
				"Scheduling conditions were not met for pod ns1/p1:",
				"PodAffinity: failed pre-predicate PodAffinity. Reasons: reason1, reason2",
				"PodFitsHostPorts: failed pre-predicate PodFitsHostPorts. Reasons: reason1, reason2",
			},
		},
	}
	for _, tt := range tests {
		t.Logf("Running test: %s", tt.name)
		t.Run(tt.name, func(t *testing.T) {
			skipPredicates := SkipPredicates{}
			got := evaluateTaskOnPrePredicate(tt.args.task, tt.args.k8sPredicates, skipPredicates)

			// I use this weird way to compare the 2 errors because sometimes the line order in
			// the returning error will change
			if (got != nil) != tt.wantErr {
				t.Errorf("evaluateTaskOnPrePredicate() = %v, want %v", got,
					strings.Join(tt.wantedErrorData, "\n"))
			}
			if got != nil {
				if tt.wantErr {
					gotErrorData := strings.Split(got.Error(), "\n")
					gotErrorData = gotErrorData[:len(gotErrorData)-1]
					sort.Strings(gotErrorData)

					wantedErrorData := make([]string, len(tt.wantedErrorData))
					copy(wantedErrorData, tt.wantedErrorData)
					sort.Strings(wantedErrorData)

					if !reflect.DeepEqual(gotErrorData, wantedErrorData) {
						t.Errorf("evaluateTaskOnPrePredicate(), Got:\n%v\nExpected:\n%v", got,
							strings.Join(tt.wantedErrorData, "\n"))
					}
				} else {
					t.Errorf("evaluateTaskOnPrePredicate() = %v, want %v", got, nil)
				}
			}
		})
	}
}

func TestMaxPodsWithReleasingPods(t *testing.T) {
	// Test that releasing pods don't count toward the max pods limit
	node := common_info.BuildNode("n1", common_info.BuildResourceList("16000m", "32G"))
	node.Status.Allocatable[v1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)

	// Create 109 running pods and 1 releasing pod
	runningPods := make([]*v1.Pod, 109)
	for i := 0; i < 109; i++ {
		runningPods[i] = common_info.BuildPod("default", fmt.Sprintf("running-pod-%d", i), "n1",
			v1.PodRunning, common_info.BuildResourceList("100m", "100M"),
			[]metav1.OwnerReference{}, map[string]string{},
			map[string]string{commonconstants.PodGroupAnnotationForPod: "job1"})
	}

	releasingPod := common_info.BuildPod("default", "releasing-pod", "n1",
		v1.PodRunning, common_info.BuildResourceList("100m", "100M"),
		[]metav1.OwnerReference{}, map[string]string{},
		map[string]string{commonconstants.PodGroupAnnotationForPod: "job1"})

	preemptorPod := common_info.BuildPod("default", "preemptor-pod", "",
		v1.PodPending, common_info.BuildResourceList("100m", "100M"),
		[]metav1.OwnerReference{}, map[string]string{},
		map[string]string{commonconstants.PodGroupAnnotationForPod: "job2"})

	// Create node info and add pods
	nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(gomock.NewController(t))
	nodePodAffinityInfo.EXPECT().AddPod(gomock.Any()).AnyTimes()

	vectorMap := resource_info.NewResourceVectorMap()
	ni := node_info.NewNodeInfo(node, nodePodAffinityInfo, vectorMap)

	// Add running pods
	for _, pod := range runningPods {
		task := pod_info.NewTaskInfo(pod, nil, vectorMap)
		task.Status = pod_status.Running
		err := ni.AddTask(task)
		if err != nil {
			t.Fatalf("Failed to add running pod: %v", err)
		}
	}

	// Add releasing pod
	releasingTask := pod_info.NewTaskInfo(releasingPod, nil, vectorMap)
	releasingTask.Status = pod_status.Releasing
	err := ni.AddTask(releasingTask)
	if err != nil {
		t.Fatalf("Failed to add releasing pod: %v", err)
	}

	// Now try to allocate the preemptor pod - it should succeed because
	// the releasing pod's resources (including its pod count) are available
	preemptorTask := pod_info.NewTaskInfo(preemptorPod, nil, vectorMap)
	preemptorTask.Status = pod_status.Pending

	// Check if the task is allocatable
	allocatable := ni.IsTaskAllocatableOnReleasingOrIdle(preemptorTask)

	// Debug output
	podsIdx := ni.VectorMap.GetIndex(v1.ResourcePods)
	t.Logf("Node Allocatable pods: %v", ni.AllocatableVector.Get(podsIdx))
	t.Logf("Node Idle pods: %v", ni.IdleVector.Get(podsIdx))
	t.Logf("Node Used pods: %v", ni.UsedVector.Get(podsIdx))
	t.Logf("Node Releasing pods: %v", ni.ReleasingVector.Get(podsIdx))
	t.Logf("Preemptor ResReqVector: %v", preemptorTask.ResReqVector)

	if !allocatable {
		t.Errorf("Preemptor pod should be allocatable (109 Running + 1 Releasing + 1 new = 110 total, but Releasing pod resources are available)")
		t.Logf("Node IdleVector: %v", ni.IdleVector)
		t.Logf("Node UsedVector: %v", ni.UsedVector)
		t.Logf("Node ReleasingVector: %v", ni.ReleasingVector)
		t.Logf("Preemptor ResReqVector: %v", preemptorTask.ResReqVector)
	}
}

func Test_predicatesPlugin_evaluateTaskOnPredicates(t *testing.T) {
	type args struct {
		taskName                                 common_info.PodID
		jobName                                  common_info.PodGroupID
		nodeName                                 string
		storageSchedulingEnabled                 bool
		isNonPreemptableTaskOnNodeOverCapacityFn api.IsTaskAllocationOverCapacityFn
		k8sPredicates                            k8s_internal.SessionPredicates
	}
	type clusterData struct {
		jobs                            []*jobs_fake.TestJobBasic
		nodes                           map[string]nodes_fake.TestNodeBasic
		isRestrictNodeSchedulingEnabled func() bool
	}
	tests := []struct {
		name        string
		args        args
		clusterData clusterData
		err         error
	}{
		{
			"Empty predicates",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"fail on non-preemptible over capacity",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysUnschedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"node does not have enough capacity. Reason: overcapacity, Details: custom error details"),
		},
		{
			"node resources type predicate - Whole GPU task on MIG node with mixed strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"non MIG jobs cant run on a node with MigStrategy='mixed'"),
		},
		{
			"node resources type predicate - Fraction task on MIG node with mixed strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"non MIG jobs cant run on a node with MigStrategy='mixed'"),
		},
		{
			"node resources type predicate - GPU memory task on MIG node with mixed strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "j1",
						RequiredGpuMemory: 500,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"non MIG jobs cant run on a node with MigStrategy='mixed'"),
		},
		{
			"node resources type predicate - MIG task on MIG node with mixed strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - CPU task on MIG node with mixed strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredCPUsPerTask: 1000,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MigInstances: map[v1.ResourceName]int{
							"nvidia.com/mig-1g.5gb": 1,
						},
						MigStrategy: node_info.MigStrategyMixed,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - Whole GPU task on MIG node with single strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs:        7,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - Fraction task on MIG node with single strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs:        7,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"only whole gpu jobs can run on a node with MigStrategy='single'"),
		},
		{
			"node resources type predicate - GPU memory task on MIG node with single strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "j1",
						RequiredGpuMemory: 500,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs:        7,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"only whole gpu jobs can run on a node with MigStrategy='single'"),
		},
		{
			"node resources type predicate - MIG task on MIG node with single strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs:        7,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"only whole gpu jobs can run on a node with MigStrategy='single'"),
		},
		{
			"node resources type predicate - CPU task on MIG node with single strategy",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredCPUsPerTask: 1000,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs:        7,
						MigStrategy: node_info.MigStrategySingle,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - Whole GPU task on non MIG node",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 1,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs: 8,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - Fraction task on non MIG node",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs: 8,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - GPU memory task on non MIG node",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "j1",
						RequiredGpuMemory: 500,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs: 8,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"node resources type predicate - MIG task on non MIG node",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								RequiredMigInstances: map[v1.ResourceName]int{
									"nvidia.com/mig-1g.5gb": 1,
								},
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs: 8,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				"MIG job cannot be scheduled on non-MIG node"),
		},
		{
			"node resources type predicate - CPU task on non MIG node",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredCPUsPerTask: 1000,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GPUs: 8,
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			nil,
		},
		{
			"fail on gpu memory not synced",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:              "j1",
						RequiredGpuMemory: 1024,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						GpuMemorySynced: ptr.To(false),
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1",
				fmt.Sprintf("node is not a gpu node or the gpu memory count on the node was not synced yet,"+
					" task: <%v/%v>, node: <%v>", "", "j1-0", "n1")),
		},
		{
			"fail on max pods - cpu only task",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MaxTaskNum: ptr.To(0),
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1", api.NodePodNumberExceeded),
		},
		{
			"fail on max pods - fraction task",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.EmptyPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name:                "j1",
						RequiredGPUsPerTask: 0.5,
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {
						MaxTaskNum: ptr.To(1),
					},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitError("j1-0", "", "n1", api.NodePodNumberExceeded),
		},
		{
			"fail on predicate HostPorts with error",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.FailingPredicateWithError(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			fmt.Errorf("failed with error predicate PodFitsHostPorts"),
		},
		{
			"fail on predicate HostPorts with error",
			args{
				taskName:                                 "j1-0",
				jobName:                                  "j1",
				nodeName:                                 "n1",
				storageSchedulingEnabled:                 false,
				isNonPreemptableTaskOnNodeOverCapacityFn: isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable,
				k8sPredicates: k8s_internal.SessionPredicates{
					predicates.PodFitsHostPorts:       predicates_fake.FailingPredicate(predicates.PodFitsHostPorts),
					predicates.PodToleratesNodeTaints: predicates_fake.EmptyPredicate(predicates.PodToleratesNodeTaints),
					predicates.NodeAffinity:           predicates_fake.EmptyPredicate(predicates.NodeAffinity),
					predicates.PodAffinity:            predicates_fake.EmptyPredicate(predicates.PodAffinity),
					predicates.VolumeBinding:          predicates_fake.EmptyPredicate(predicates.VolumeBinding),
					predicates.NodeScheduler:          predicates_fake.EmptyPredicate(predicates.NodeScheduler),
				},
			},
			clusterData{
				jobs: []*jobs_fake.TestJobBasic{
					{
						Name: "j1",
						Tasks: []*tasks_fake.TestTaskBasic{
							{
								NodeName: "n1",
							},
						},
					},
				},
				nodes: map[string]nodes_fake.TestNodeBasic{
					"n1": {},
				},
				isRestrictNodeSchedulingEnabled: func() bool {
					return false
				},
			},
			common_info.NewFitErrorByReasons("j1-0", "", "n1",
				fmt.Errorf("failed predicate PodFitsHostPorts"), "reason1", "reason2", "reason3"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			jobsMap, tasksMap, _ := jobs_fake.BuildJobsAndTasksMaps(tt.clusterData.jobs, vectorMap)
			nodesMap := nodes_fake.BuildNodesInfoMap(tt.clusterData.nodes, tasksMap, nil, vectorMap)
			ssn := &framework.Session{
				ClusterInfo: &api.ClusterInfo{
					Nodes:         nodesMap,
					PodGroupInfos: jobsMap,
					Queues:        map[common_info.QueueID]*queue_info.QueueInfo{},
				},
			}
			pp := &predicatesPlugin{
				storageSchedulingEnabled: tt.args.storageSchedulingEnabled,
				ssn:                      ssn,
			}
			skipPredicates := SkipPredicates{}

			task := jobsMap[tt.args.jobName].GetAllPodsMap()[tt.args.taskName]
			job := jobsMap[tt.args.jobName]
			node := nodesMap[tt.args.nodeName]

			if err := pp.evaluateTaskOnPredicates(
				task, job, node, tt.args.k8sPredicates,
				tt.args.isNonPreemptableTaskOnNodeOverCapacityFn,
				tt.clusterData.isRestrictNodeSchedulingEnabled,
				skipPredicates,
			); !reflect.DeepEqual(err, tt.err) {
				t.Errorf("evaluateTaskOnPredicates() error:\n%v\nExpected: %v", err, tt.err)
			}
		})
	}
}

func isNonPreemptableTaskOnNodeOverCapacityFnAlwaysUnschedulable(
	_ *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, _ *node_info.NodeInfo,
) *api.SchedulableResult {
	return &api.SchedulableResult{
		IsSchedulable: false,
		Reason:        "overcapacity",
		Message:       "custom error details",
		Details:       nil,
	}
}

func isNonPreemptableTaskOnNodeOverCapacityFnAlwaysSchedulable(
	_ *pod_info.PodInfo, _ *podgroup_info.PodGroupInfo, _ *node_info.NodeInfo,
) *api.SchedulableResult {
	return &api.SchedulableResult{
		IsSchedulable: true,
		Reason:        "",
		Message:       "",
		Details:       nil,
	}
}
