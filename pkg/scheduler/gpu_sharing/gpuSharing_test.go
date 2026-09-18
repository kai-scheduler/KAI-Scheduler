// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpu_sharing

import (
	"testing"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func Test_getNodePreferableGpuForSharing(t *testing.T) {
	type args struct {
		fittingGPUsOnNode []string
		node              *node_info.NodeInfo
		nodeSharingInfo   *node_info.GpuSharingNodeInfo
		pod               *pod_info.PodInfo
		isPipelineOnly    bool
	}
	type want struct {
		groupLength          int
		expectedGroupsInList []string
		isReleasing          bool
	}
	tests := []struct {
		name string
		args args
		want want
	}{
		{
			name: "one whole gpu",
			args: args{
				fittingGPUsOnNode: []string{pod_info.WholeGpuIndicator},
				node: func() *node_info.NodeInfo {
					n := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "n1",
							Annotations: map[string]string{},
						},
						Spec: v1.NodeSpec{},
						Status: v1.NodeStatus{
							Allocatable: map[v1.ResourceName]resource.Quantity{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("10G"),
								"nvidia.com/gpu":  resource.MustParse("1"),
								v1.ResourcePods:   resource.MustParse("110"),
							},
						},
					}
					vectorMap := resource_info.NewResourceVectorMap()
					for resourceName := range n.Status.Allocatable {
						vectorMap.AddResource(resourceName)
					}
					return node_info.NewNodeInfo(n, nil, vectorMap)
				}(),
				nodeSharingInfo: nil,
				pod: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "p1",
						Annotations: map[string]string{},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				}, resource_info.NewResourceVectorMap()),
				isPipelineOnly: false,
			},
			want: want{
				groupLength:          1,
				expectedGroupsInList: make([]string, 0),
				isReleasing:          false,
			},
		},
		{
			name: "one fraction gpu - one gpu free on node",
			args: args{
				fittingGPUsOnNode: []string{pod_info.WholeGpuIndicator},
				node: func() *node_info.NodeInfo {
					n := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "n1",
							Annotations: map[string]string{},
						},
						Spec: v1.NodeSpec{},
						Status: v1.NodeStatus{
							Allocatable: map[v1.ResourceName]resource.Quantity{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("10G"),
								"nvidia.com/gpu":  resource.MustParse("1"),
								v1.ResourcePods:   resource.MustParse("110"),
							},
						},
					}
					vectorMap := resource_info.NewResourceVectorMap()
					for resourceName := range n.Status.Allocatable {
						vectorMap.AddResource(resourceName)
					}
					return node_info.NewNodeInfo(n, nil, vectorMap)
				}(),
				nodeSharingInfo: nil,
				pod: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "p1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
							commonconstants.GpuFraction:              "0.5",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				}, resource_info.NewResourceVectorMap()),
				isPipelineOnly: false,
			},
			want: want{
				groupLength:          1,
				expectedGroupsInList: make([]string, 0),
				isReleasing:          false,
			},
		},
		{
			name: "one fraction gpu - half gpu free on node",
			args: args{
				fittingGPUsOnNode: []string{"0", pod_info.WholeGpuIndicator},
				node: func() *node_info.NodeInfo {
					n := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "n1",
							Annotations: map[string]string{},
						},
						Spec: v1.NodeSpec{},
						Status: v1.NodeStatus{
							Allocatable: map[v1.ResourceName]resource.Quantity{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("10G"),
								"nvidia.com/gpu":  resource.MustParse("2"),
								v1.ResourcePods:   resource.MustParse("110"),
							},
						},
					}
					vectorMap := resource_info.NewResourceVectorMap()
					for resourceName := range n.Status.Allocatable {
						vectorMap.AddResource(resourceName)
					}
					return node_info.NewNodeInfo(n, nil, vectorMap)
				}(),
				nodeSharingInfo: func() *node_info.GpuSharingNodeInfo {
					sharingMaps := &node_info.GpuSharingNodeInfo{
						ReleasingSharedGPUs:       make(map[string]bool),
						UsedSharedGPUsMemory:      make(map[string]int64),
						ReleasingSharedGPUsMemory: make(map[string]int64),
						AllocatedSharedGPUsMemory: make(map[string]int64),
					}
					sharingMaps.ReleasingSharedGPUs["0"] = true
					sharingMaps.ReleasingSharedGPUsMemory["0"] = 50
					sharingMaps.UsedSharedGPUsMemory["0"] = 50
					sharingMaps.AllocatedSharedGPUsMemory["0"] = 50
					return sharingMaps
				}(),
				pod: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "p1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
							commonconstants.GpuFraction:              "0.5",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				}, resource_info.NewResourceVectorMap()),
				isPipelineOnly: false,
			},
			want: want{
				groupLength:          1,
				expectedGroupsInList: []string{"0"},
				isReleasing:          true,
			},
		},
		{
			name: "multi fraction gpu - one gpu free on node",
			args: args{
				fittingGPUsOnNode: []string{"0", pod_info.WholeGpuIndicator, pod_info.WholeGpuIndicator},
				node: func() *node_info.NodeInfo {
					n := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "n1",
							Annotations: map[string]string{},
						},
						Spec: v1.NodeSpec{},
						Status: v1.NodeStatus{
							Allocatable: map[v1.ResourceName]resource.Quantity{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("10G"),
								"nvidia.com/gpu":  resource.MustParse("3"),
								v1.ResourcePods:   resource.MustParse("110"),
							},
						},
					}
					vectorMap := resource_info.NewResourceVectorMap()
					for resourceName := range n.Status.Allocatable {
						vectorMap.AddResource(resourceName)
					}
					return node_info.NewNodeInfo(n, nil, vectorMap)
				}(),
				nodeSharingInfo: func() *node_info.GpuSharingNodeInfo {
					sharingMaps := &node_info.GpuSharingNodeInfo{
						ReleasingSharedGPUs:       make(map[string]bool),
						UsedSharedGPUsMemory:      make(map[string]int64),
						ReleasingSharedGPUsMemory: make(map[string]int64),
						AllocatedSharedGPUsMemory: make(map[string]int64),
					}
					sharingMaps.ReleasingSharedGPUs["0"] = true
					sharingMaps.ReleasingSharedGPUsMemory["0"] = 50
					sharingMaps.UsedSharedGPUsMemory["0"] = 50
					sharingMaps.AllocatedSharedGPUsMemory["0"] = 50
					return sharingMaps
				}(),
				pod: pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "p1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
							commonconstants.GpuFraction:              "0.5",
							commonconstants.GpuFractionsNumDevices:   "2",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				}, resource_info.NewResourceVectorMap()),
				isPipelineOnly: false,
			},
			want: want{
				groupLength:          2,
				expectedGroupsInList: []string{"0"},
				isReleasing:          true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpusForSharing := GetNodePreferableGpuForSharing(
				tt.args.fittingGPUsOnNode, tt.args.node, tt.args.pod, tt.args.isPipelineOnly)

			if gpusForSharing == nil {
				if tt.want.groupLength > 0 {
					t.Errorf(
						"getNodePreferableGpuForSharing() couldn't find any gpus fo sharing. Expected %d groups",
						tt.want.groupLength)
				}
				return
			}
			if gpusForSharing.IsReleasing != tt.want.isReleasing {
				t.Errorf("getNodePreferableGpuForSharing().IsReleasing = %v, want %v",
					tt.want.isReleasing, gpusForSharing.IsReleasing)
			}
			gpuGroupIDs := make([]string, 0, len(gpusForSharing.FractionalGpuGroups))
			for _, fractionalGpuGroup := range gpusForSharing.FractionalGpuGroups {
				gpuGroupIDs = append(gpuGroupIDs, fractionalGpuGroup.ID)
			}
			if len(gpuGroupIDs) != tt.want.groupLength {
				t.Errorf("getNodePreferableGpuForSharing() groups array %v, wanted length %v",
					gpuGroupIDs, tt.want.groupLength)
			}
			if tt.want.expectedGroupsInList != nil {
				for _, expectedGroup := range tt.want.expectedGroupsInList {
					if !slices.Contains(gpuGroupIDs, expectedGroup) {
						t.Errorf("getNodePreferableGpuForSharing() groups array %v, expected to include %v",
							gpuGroupIDs, expectedGroup)
					}
				}
			}
		})
	}
}

// GetNodePreferableGpuForSharing is handed an already-filtered list, so the
// compute-exhausted case shows up here as the GPU being marked releasing: the
// task gets pipelined behind the SMs freeing up rather than allocated onto them.
func Test_getNodePreferableGpuForSharing_computeExhaustedGpuIsReleasing(t *testing.T) {
	const (
		gpuMemoryMiB = int64(23028)
		gpuGroup     = "gpu-group-0"
	)

	vectorMap := resource_info.NewResourceVectorMap()
	// No whole GPU left idle, so the shared device is the only placement option.
	nodeResources := resource_info.NewResource(0, 0, 0)
	nodeResources.ScalarResources()[resource_info.PodsResourceName] = 10
	node := &node_info.NodeInfo{
		Name:                    "n1",
		PodInfos:                map[common_info.PodID]*pod_info.PodInfo{},
		VectorMap:               vectorMap,
		MemoryOfEveryGpuOnNode:  gpuMemoryMiB,
		ComputeOfEveryGpuOnNode: node_info.WholeGpuCompute,
		IdleVector:              nodeResources.ToVector(vectorMap),
		ReleasingVector:         resource_info.NewResourceVector(vectorMap),
		AllocatableVector:       nodeResources.ToVector(vectorMap),
		UsedVector:              resource_info.NewResourceVector(vectorMap),
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			ReleasingSharedGPUs:        map[string]bool{},
			UsedSharedGPUsMemory:       map[string]int64{gpuGroup: gpuMemoryMiB / 5},
			ReleasingSharedGPUsMemory:  map[string]int64{},
			AllocatedSharedGPUsMemory:  map[string]int64{gpuGroup: gpuMemoryMiB / 5},
			UsedSharedGPUsCompute:      map[string]int64{gpuGroup: 80},
			ReleasingSharedGPUsCompute: map[string]int64{},
			AllocatedSharedGPUsCompute: map[string]int64{gpuGroup: 80},
			DRASharedDeviceRefCount:    map[string]int{},
		},
	}

	smSharingPod := func(name, computeRequest string) *pod_info.PodInfo {
		return pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				UID:       types.UID(name),
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod:                        "pg-" + name,
					commonconstants.GpuFraction:                                     "0.2",
					resources.CalcGpuComputeRequestAnnotationForContainer("c1"):     computeRequest,
					resources.CalcGpuComputeSharingModeAnnotationForContainer("c1"): string(schedulingv1alpha2.GPUComputeSharingModeSMSharing),
				},
			},
			Spec:   v1.PodSpec{Containers: []v1.Container{{Name: "c1"}}},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}, vectorMap)
	}

	placed := smSharingPod("placed", "0.8")
	placed.SetGPUGroupIDs([]string{gpuGroup})
	node.PodInfos[common_info.PodID("default/placed")] = placed

	overCommitting := GetNodePreferableGpuForSharing([]string{gpuGroup}, node, smSharingPod("over-committing", "0.8"), false)
	if overCommitting == nil || !overCommitting.IsReleasing {
		t.Errorf("a compute-exhausted GPU must be offered only as releasing, got %+v", overCommitting)
	}

	fitting := GetNodePreferableGpuForSharing([]string{gpuGroup}, node, smSharingPod("fitting", "0.2"), false)
	if fitting == nil || fitting.IsReleasing {
		t.Errorf("a GPU with compute left must be offered for immediate allocation, got %+v", fitting)
	}
}
