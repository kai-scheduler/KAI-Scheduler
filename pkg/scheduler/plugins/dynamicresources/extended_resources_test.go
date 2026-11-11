// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

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

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

func TestGpuRequestHasNoDeviceSource(t *testing.T) {
	gpuMappedToDRA := extendedresourcecache.NewExtendedResourceCache(klog.Background())
	gpuMappedToDRA.OnAdd(&resourceapi.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu.nvidia.com"},
		Spec:       resourceapi.DeviceClassSpec{ExtendedResourceName: ptr.To("nvidia.com/gpu")},
	}, false)
	noMapping := extendedresourcecache.NewExtendedResourceCache(klog.Background())

	gpuPod := func(gpus string) *pod_info.PodInfo {
		return &pod_info.PodInfo{Pod: &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{
			Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nvidia.com/gpu": resource.MustParse(gpus)}},
		}}}}}
	}
	cpuPod := &pod_info.PodInfo{Pod: &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{
		Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}},
	}}}}}
	node := func(hasDRAGPUs bool, allocatable v1.ResourceList) *node_info.NodeInfo {
		return &node_info.NodeInfo{
			Name:       "node0",
			HasDRAGPUs: hasDRAGPUs,
			Node:       &v1.Node{Status: v1.NodeStatus{Allocatable: allocatable}},
		}
	}

	for name, test := range map[string]struct {
		task *pod_info.PodInfo
		node *node_info.NodeInfo
		dbc  *extendedresourcecache.ExtendedResourceCache
		want bool
	}{
		"GPU request on a DRA-only node with no DeviceClass mapping": {
			task: gpuPod("1"), node: node(true, v1.ResourceList{}), dbc: noMapping, want: true,
		},
		"nil extended resource cache": {
			task: gpuPod("1"), node: node(true, v1.ResourceList{}), dbc: nil, want: true,
		},
		"DeviceClass maps nvidia.com/gpu to DRA": {
			task: gpuPod("1"), node: node(true, v1.ResourceList{}), dbc: gpuMappedToDRA, want: false,
		},
		"node also has device plugin GPUs": {
			task: gpuPod("1"), node: node(true, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("4")}), dbc: noMapping, want: false,
		},
		"node with zero device plugin GPUs reported": {
			task: gpuPod("1"), node: node(true, v1.ResourceList{"nvidia.com/gpu": resource.MustParse("0")}), dbc: noMapping, want: true,
		},
		"node without DRA GPUs": {
			task: gpuPod("1"), node: node(false, v1.ResourceList{}), dbc: noMapping, want: false,
		},
		"pod that does not request GPUs": {
			task: cpuPod, node: node(true, v1.ResourceList{}), dbc: noMapping, want: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, gpuRequestHasNoDeviceSource(test.task, test.node, test.dbc))
		})
	}
}
