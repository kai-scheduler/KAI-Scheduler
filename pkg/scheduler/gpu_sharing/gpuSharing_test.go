// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpu_sharing

import (
	"testing"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
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
						vectorMap.AddResource(string(resourceName))
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
				}, nil, resource_info.NewResourceVectorMap()),
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
						vectorMap.AddResource(string(resourceName))
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
				}, nil, resource_info.NewResourceVectorMap()),
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
						vectorMap.AddResource(string(resourceName))
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
				}, nil, resource_info.NewResourceVectorMap()),
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
						vectorMap.AddResource(string(resourceName))
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
				}, nil, resource_info.NewResourceVectorMap()),
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
			if len(gpusForSharing.Groups) != tt.want.groupLength {
				t.Errorf("getNodePreferableGpuForSharing() groups array %v, wanted length %v",
					gpusForSharing.Groups, tt.want.groupLength)
			}
			if tt.want.expectedGroupsInList != nil {
				for _, expectedGroup := range tt.want.expectedGroupsInList {
					if !slices.Contains(gpusForSharing.Groups, expectedGroup) {
						t.Errorf("getNodePreferableGpuForSharing() groups array %v, expected to include %v",
							gpusForSharing.Groups, expectedGroup)
					}
				}
			}
		})
	}
}
