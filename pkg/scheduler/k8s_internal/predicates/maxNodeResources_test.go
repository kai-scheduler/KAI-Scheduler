// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ksf "k8s.io/kube-scheduler/framework"

	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func Test_podToMaxNodeResourcesFiltering(t *testing.T) {
	type args struct {
		nodePoolName string
		nodesMap     map[string]*node_info.NodeInfo
		pod          *v1.Pod
	}
	type expected struct {
		status *ksf.Status
	}
	tests := []struct {
		name     string
		args     args
		expected expected
	}{
		{
			"small pod",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										v1.ResourceCPU: resource.MustParse("20m"),
									},
								},
							},
						},
					},
				},
			},
			expected{
				nil,
			},
		},
		{
			"not enough cpu",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										v1.ResourceCPU: resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expected{
				ksf.NewStatus(ksf.Unschedulable,
					"The pod n1/name1 requires GPU: 0, CPU: 1 (cores), memory: 0 (GB). Max CPU resources available in a single node in the default node-pool is topped at 0.5 cores"),
			},
		},
		{
			"not enough memory",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("400Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										v1.ResourceMemory: resource.MustParse("1G"),
									},
								},
							},
						},
					},
				},
			},
			expected{
				ksf.NewStatus(ksf.Unschedulable,
					"The pod n1/name1 requires GPU: 0, CPU: 0 (cores), memory: 1 (GB). Max memory resources available in a single node in the default node-pool is topped at 0.419 GB"),
			},
		},
		{
			"not enough whole gpus",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										resource_info.GPUResourceName: resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			expected{
				ksf.NewStatus(ksf.Unschedulable,
					"The pod n1/name1 requires GPU: 2, CPU: 0 (cores), memory: 0 (GB). Max GPU resources available in a single node in the default node-pool is topped at 1"),
			},
		},
		{
			"not enough fraction gpu",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("0"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("0"),
							"kai.scheduler/r1":            resource.MustParse("2"),
						}),
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
							common_info.GPUFraction:                  "0.5",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				},
			},
			expected{
				ksf.NewStatus(ksf.Unschedulable,
					"The pod n1/name1 requires GPU: 0.5, CPU: 0 (cores), memory: 0 (GB). No node in the default node-pool has GPU resources"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mnr := NewMaxNodeResourcesPredicate(tt.args.nodesMap, tt.args.nodePoolName)
			if _, status := mnr.PreFilter(nil, nil, tt.args.pod, nil); !reflect.DeepEqual(status, tt.expected.status) {
				t.Errorf("PreFilter() = %v, want %v", status, tt.expected.status)
			}
		})
	}
}
