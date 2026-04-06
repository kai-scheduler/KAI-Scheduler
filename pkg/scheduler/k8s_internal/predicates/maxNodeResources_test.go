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

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

func buildTestNodes(nodesResourceLists map[string]v1.ResourceList) map[string]*node_info.NodeInfo {
	var allLists []v1.ResourceList
	for _, rl := range nodesResourceLists {
		allLists = append(allLists, rl)
	}
	vm := resource_info.BuildResourceVectorMap(allLists)
	result := make(map[string]*node_info.NodeInfo, len(nodesResourceLists))
	for name, rl := range nodesResourceLists {
		result[name] = &node_info.NodeInfo{
			AllocatableVector: resource_info.NewResourceVectorFromResourceList(rl, vm),
			VectorMap:         vm,
		}
	}
	return result
}

func Test_podToMaxNodeResourcesFiltering(t *testing.T) {
	type args struct {
		nodePoolName       string
		nodesResourceLists map[string]v1.ResourceList
		resourceClaims     []*resourceapi.ResourceClaim
		pod                *v1.Pod
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
					},
					"n2": {
						v1.ResourceCPU:                resource.MustParse("500m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("400Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
					},
					"n2": {
						v1.ResourceCPU:                resource.MustParse("500m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
					},
					"n2": {
						v1.ResourceCPU:                resource.MustParse("500m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("0"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
					},
					"n2": {
						v1.ResourceCPU:                resource.MustParse("500m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("0"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
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
				nodesResourceLists: map[string]v1.ResourceList{
					"n1": {
						v1.ResourceCPU:                resource.MustParse("100m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
						v1.ResourceEphemeralStorage:   resource.MustParse("10Gi"),
					},
					"n2": {
						v1.ResourceCPU:                resource.MustParse("500m"),
						v1.ResourceMemory:             resource.MustParse("200Mi"),
						resource_info.GPUResourceName: resource.MustParse("1"),
						"kai.scheduler/r1":            resource.MustParse("2"),
						v1.ResourcePods:               resource.MustParse("110"),
						v1.ResourceEphemeralStorage:   resource.MustParse("20Gi"),
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
			nodesMap := buildTestNodes(tt.args.nodesResourceLists)
			mnr := NewMaxNodeResourcesPredicate(nodesMap, tt.args.resourceClaims, tt.args.nodePoolName)
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
	var allLists []v1.ResourceList
	for _, rl := range nodeBases {
		allLists = append(allLists, rl)
	}
	vm := resource_info.BuildResourceVectorMap(allLists)
	nodesMap := make(map[string]*node_info.NodeInfo)
	for nodeName, baseList := range nodeBases {
		allocVec := resource_info.NewResourceVectorFromResourceList(baseList, vm)
		ni := &node_info.NodeInfo{
			Name:              nodeName,
			AllocatableVector: allocVec,
			IdleVector:        allocVec.Clone(),
			ReleasingVector:   resource_info.NewResourceVector(vm),
			UsedVector:        resource_info.NewResourceVector(vm),
			VectorMap:         vm,
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
