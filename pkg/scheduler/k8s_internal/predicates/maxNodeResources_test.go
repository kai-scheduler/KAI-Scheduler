// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"context"
	"strconv"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/ptr"

	commonconstants "github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

func Test_podToMaxNodeResourcesFiltering(t *testing.T) {
	type args struct {
		nodePoolName   string
		nodesMap       map[string]*node_info.NodeInfo
		resourceClaims []*resourceapi.ResourceClaim
		pod            *v1.Pod
	}
	type expected struct {
		status         *ksf.Status
		skipExactMatch bool
		partialReasons []string
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
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
				false,
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
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
					"The pod n1/name1 requires GPU: 0, CPU: 1 (cores), memory: 0 (GB), pods: 1. Max CPU resources available in a single node in the default node-pool is topped at 0.5 cores"),
				false,
				nil,
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
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
					"The pod n1/name1 requires GPU: 0, CPU: 0 (cores), memory: 1 (GB), pods: 1. Max memory resources available in a single node in the default node-pool is topped at 0.419 GB"),
				false,
				nil,
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
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
					"The pod n1/name1 requires GPU: 2, CPU: 0 (cores), memory: 0 (GB), pods: 1. Max GPU resources available in a single node in the default node-pool is topped at 1"),
				false,
				nil,
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
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("0"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
					"The pod n1/name1 requires GPU: 0.5, CPU: 0 (cores), memory: 0 (GB), pods: 1. No node in the default node-pool has GPU resources"),
				false,
				nil,
			},
		},
		{
			"not enough ephemeral storage",
			args{
				nodesMap: map[string]*node_info.NodeInfo{
					"n1": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("100m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourceEphemeralStorage:   resource.MustParse("10Gi"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
					"n2": {
						Allocatable: resource_info.ResourceFromResourceList(v1.ResourceList{
							v1.ResourceCPU:                resource.MustParse("500m"),
							v1.ResourceMemory:             resource.MustParse("200Mi"),
							resource_info.GPUResourceName: resource.MustParse("1"),
							"kai.scheduler/r1":            resource.MustParse("2"),
							v1.ResourceEphemeralStorage:   resource.MustParse("20Gi"),
							v1.ResourcePods:               resource.MustParse("110"),
						}),
					},
				},
				resourceClaims: []*resourceapi.ResourceClaim{},
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
										v1.ResourceEphemeralStorage: resource.MustParse("25G"),
									},
								},
							},
						},
					},
				},
			},
			expected{
				nil,
				true,
				[]string{
					"The pod n1/name1 requires",
					"ephemeral-storage: 25 (GB)",
					"pods: 1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, node := range tt.args.nodesMap {
				if _, found := node.Allocatable.ScalarResources()[v1.ResourcePods]; !found {
					node.Allocatable.ScalarResources()[v1.ResourcePods] = 110
				}
			}
			mnr := NewMaxNodeResourcesPredicate(tt.args.nodesMap, tt.args.resourceClaims, tt.args.nodePoolName)
			_, status := mnr.PreFilter(context.TODO(), nil, tt.args.pod, nil)
			if !tt.expected.skipExactMatch && !statusEqual(status, tt.expected.status) {
				t.Errorf("PreFilter() = %v, want %v", status, tt.expected.status)
			}
			if len(tt.expected.partialReasons) > 0 && status == nil {
				t.Fatalf("PreFilter() returned nil status, but expected partial reasons: %v", strings.Join(tt.expected.partialReasons, ", "))
			}
			for _, partialReason := range tt.expected.partialReasons {
				if !strings.Contains(status.Message(), partialReason) {
					t.Errorf("PreFilter() = %v, missing partial reason: %v", status.Message(), partialReason)
				}
			}
		})
	}
}

func makeDRAResourceSlice(name, nodeName, driver string, deviceCount int) *resourceapi.ResourceSlice {
	devices := make([]resourceapi.Device, deviceCount)
	for i := 0; i < deviceCount; i++ {
		devices[i] = resourceapi.Device{Name: name + "-device-" + strconv.Itoa(i)}
	}
	return &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourceapi.ResourceSliceSpec{
			NodeName: ptr.To(nodeName),
			Driver:   driver,
			Devices:  devices,
		},
	}
}

func buildNodesFromResourceSlices(slices []*resourceapi.ResourceSlice, nodeBases map[string]v1.ResourceList) map[string]*node_info.NodeInfo {
	slicesByNode := make(map[string][]*resourceapi.ResourceSlice)
	for _, slice := range slices {
		if slice.Spec.NodeName != nil {
			slicesByNode[*slice.Spec.NodeName] = append(slicesByNode[*slice.Spec.NodeName], slice)
		}
	}
	nodesMap := make(map[string]*node_info.NodeInfo)
	for nodeName, baseList := range nodeBases {
		allocatable := resource_info.ResourceFromResourceList(baseList)
		idle := allocatable.Clone()
		vectorMap := resource_info.NewResourceVectorMap()
		vectorMap.AddResourceList(baseList)
		ni := &node_info.NodeInfo{
			Name:              nodeName,
			Allocatable:       allocatable,
			Idle:              idle,
			Releasing:         resource_info.EmptyResource(),
			Used:              resource_info.EmptyResource(),
			VectorMap:         vectorMap,
			AllocatableVector: allocatable.ToVector(vectorMap),
			IdleVector:        idle.ToVector(vectorMap),
			ReleasingVector:   resource_info.NewResourceVector(vectorMap),
			UsedVector:        resource_info.NewResourceVector(vectorMap),
		}
		var draGPUCount int64
		for _, slice := range slicesByNode[nodeName] {
			if resources.IsGPUDeviceClass(slice.Spec.Driver) {
				draGPUCount += int64(len(slice.Spec.Devices))
			}
		}
		if draGPUCount > 0 {
			ni.AddDRAGPUs(float64(draGPUCount))
			ni.HasDRAGPUs = true
		}
		nodesMap[nodeName] = ni
	}
	return nodesMap
}

// TestMaxNodeResourcesPredicateDRA tests PreFilter with pods that use DRA ResourceClaims.
// Node GPU capacity is derived from ResourceSlices (DRA) instead of extended resource nvidia.com/gpu.
func TestMaxNodeResourcesPredicateDRA(t *testing.T) {
	type args struct {
		nodesMap       map[string]*node_info.NodeInfo
		resourceClaims []*resourceapi.ResourceClaim
		pod            *v1.Pod
	}
	tests := []struct {
		name     string
		args     args
		expected *ksf.Status
	}{
		{
			"DRA claim within max node resources",
			args{
				nodesMap: buildNodesFromResourceSlices(
					[]*resourceapi.ResourceSlice{
						makeDRAResourceSlice("slice-n1", "n1", "nvidia.com/gpu", 1),
						makeDRAResourceSlice("slice-n2", "n2", "nvidia.com/gpu", 1),
					},
					map[string]v1.ResourceList{
						"n1": {
							v1.ResourceCPU:     resource.MustParse("100m"),
							v1.ResourceMemory:  resource.MustParse("200Mi"),
							"kai.scheduler/r1": resource.MustParse("2"),
							"pods":             resource.MustParse("110"),
						},
						"n2": {
							v1.ResourceCPU:     resource.MustParse("500m"),
							v1.ResourceMemory:  resource.MustParse("200Mi"),
							"kai.scheduler/r1": resource.MustParse("2"),
							"pods":             resource.MustParse("110"),
						},
					},
				),
				resourceClaims: []*resourceapi.ResourceClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "gpu-claim-1",
							Namespace: "n1",
						},
						Spec: resourceapi.ResourceClaimSpec{
							Devices: resourceapi.DeviceClaim{
								Requests: []resourceapi.DeviceRequest{
									{
										Name: "gpu-req",
										Exactly: &resourceapi.ExactDeviceRequest{
											DeviceClassName: "nvidia.com/gpu",
											AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
											Count:           1,
										},
									},
								},
							},
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "gpu-claim",
								ResourceClaimName: ptr.To("gpu-claim-1"),
							},
						},
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				},
			},
			nil,
		},
		{
			"DRA claim exceeds max node resources",
			args{
				nodesMap: buildNodesFromResourceSlices(
					[]*resourceapi.ResourceSlice{
						makeDRAResourceSlice("slice-n1", "n1", "nvidia.com/gpu", 1),
						makeDRAResourceSlice("slice-n2", "n2", "nvidia.com/gpu", 1),
					},
					map[string]v1.ResourceList{
						"n1": {
							v1.ResourceCPU:     resource.MustParse("100m"),
							v1.ResourceMemory:  resource.MustParse("200Mi"),
							"kai.scheduler/r1": resource.MustParse("2"),
							"pods":             resource.MustParse("110"),
						},
						"n2": {
							v1.ResourceCPU:     resource.MustParse("500m"),
							v1.ResourceMemory:  resource.MustParse("200Mi"),
							"kai.scheduler/r1": resource.MustParse("2"),
							"pods":             resource.MustParse("110"),
						},
					},
				),
				resourceClaims: []*resourceapi.ResourceClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "gpu-claim-2",
							Namespace: "n1",
						},
						Spec: resourceapi.ResourceClaimSpec{
							Devices: resourceapi.DeviceClaim{
								Requests: []resourceapi.DeviceRequest{
									{
										Name: "gpu-req",
										Exactly: &resourceapi.ExactDeviceRequest{
											DeviceClassName: "nvidia.com/gpu",
											AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
											Count:           2,
										},
									},
								},
							},
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
					},
					Spec: v1.PodSpec{
						ResourceClaims: []v1.PodResourceClaim{
							{
								Name:              "gpu-claim",
								ResourceClaimName: ptr.To("gpu-claim-2"),
							},
						},
						Containers: []v1.Container{
							{
								Name: "c1",
							},
						},
					},
				},
			},
			ksf.NewStatus(ksf.Unschedulable,
				"The pod n1/name1 requires GPU: 2, CPU: 0 (cores), memory: 0 (GB), pods: 1. Max GPU resources available in a single node in the default node-pool is topped at 1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mnr := NewMaxNodeResourcesPredicate(tt.args.nodesMap, tt.args.resourceClaims, "")
			_, status := mnr.PreFilter(context.TODO(), nil, tt.args.pod, nil)
			if !statusEqual(status, tt.expected) {
				t.Errorf("PreFilter() = %v, want %v", status, tt.expected)
			}
		})
	}
}

func statusEqual(a, b *ksf.Status) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Code() == b.Code() && a.Message() == b.Message()
}
