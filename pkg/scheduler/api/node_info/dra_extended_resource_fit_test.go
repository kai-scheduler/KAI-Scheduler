// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/dynamic-resource-allocation/deviceclass/extendedresourcecache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

func TestRegularPodFitsDRABackedExtendedResources(t *testing.T) {
	const nicResource v1.ResourceName = "example.com/nic"
	tests := []struct {
		name           string
		mapped         bool
		requests       v1.ResourceList
		nicAllocatable int64
		nicIdle        int64
		cpuIdle        string
		cpuReleasing   string
		wantIdle       bool
		wantReleasing  bool
	}{
		{
			name: "NIC-only pod", mapped: true,
			wantIdle: true, wantReleasing: true,
		},
		{
			name: "whole-GPU pod with NIC", mapped: true,
			requests: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
			wantIdle: true, wantReleasing: true,
		},
		{
			name: "unmapped resource is still rejected",
		},
		{
			name: "mapped resource with exhausted device-plugin capacity is still rejected", mapped: true,
			nicAllocatable: 1,
		},
		{
			name: "mapped resource with available device-plugin capacity", mapped: true,
			nicAllocatable: 1, nicIdle: 1,
			wantIdle: true, wantReleasing: true,
		},
		{
			name: "CPU shortage is still rejected", mapped: true,
			requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("3")},
		},
		{
			name: "memory shortage is still rejected", mapped: true,
			requests: v1.ResourceList{v1.ResourceMemory: resource.MustParse("8Gi")},
		},
		{
			name: "GPU shortage is still rejected", mapped: true,
			requests: v1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
		},
		{
			name: "releasing CPU can satisfy the request", mapped: true,
			cpuIdle: "0", cpuReleasing: "2", wantReleasing: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			vectorMap.AddResource(nicResource)
			nicIndex := vectorMap.GetIndex(nicResource)
			node := nodes_fake.BuildNodesInfoMap(map[string]nodes_fake.TestNodeBasic{
				"node": {GPUs: 1, CPUMillis: 2, CPUMemory: 4e9, MaxTaskNum: ptr.To(10)},
			}, nil, nil, vectorMap)["node"]
			if tt.mapped {
				node.DeviceClassByResource = extendedresourcecache.NewExtendedResourceCache(klog.Background())
				node.DeviceClassByResource.OnAdd(&resourceapi.DeviceClass{
					ObjectMeta: metav1.ObjectMeta{Name: "nic"},
					Spec:       resourceapi.DeviceClassSpec{ExtendedResourceName: ptr.To(string(nicResource))},
				}, false)
			}
			node.AllocatableVector.Set(nicIndex, float64(tt.nicAllocatable))
			node.IdleVector.Set(nicIndex, float64(tt.nicIdle))
			if tt.cpuIdle != "" {
				quantity := resource.MustParse(tt.cpuIdle)
				node.IdleVector.Set(resource_info.CPUIndex, float64(quantity.MilliValue()))
			}
			if tt.cpuReleasing != "" {
				quantity := resource.MustParse(tt.cpuReleasing)
				node.ReleasingVector.Set(resource_info.CPUIndex, float64(quantity.MilliValue()))
			}
			requests := v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("500m"), v1.ResourceMemory: resource.MustParse("1Gi"),
				nicResource: resource.MustParse("1"),
			}
			for name, quantity := range tt.requests {
				requests[name] = quantity
			}
			task := pod_info.NewTaskInfo(&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "test", UID: "pod"},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "main", Resources: v1.ResourceRequirements{Requests: requests}}}},
				Status:     v1.PodStatus{Phase: v1.PodPending},
			}, vectorMap)
			assert.True(t, task.IsRegularGPURequest())
			assert.Equal(t, tt.wantIdle, node.IsTaskAllocatable(task), "idle resources")
			assert.Equal(t, tt.wantReleasing, node.IsTaskAllocatableOnReleasingOrIdle(task), "idle and releasing resources")
			if tt.wantIdle {
				assert.Nil(t, node.FittingError(task, false))
			}
		})
	}
}
