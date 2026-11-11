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
)

func podRequesting(main v1.ResourceList, init ...v1.ResourceList) *v1.Pod {
	pod := &v1.Pod{Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: main}}}}}
	for _, requests := range init {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, v1.Container{Resources: v1.ResourceRequirements{Requests: requests}})
	}
	return pod
}

func gpus(resources map[string]string) v1.ResourceList {
	list := v1.ResourceList{}
	for name, quantity := range resources {
		list[v1.ResourceName(name)] = resource.MustParse(quantity)
	}
	return list
}

func TestRequestedGpuResources(t *testing.T) {
	for name, test := range map[string]struct {
		pod  *v1.Pod
		want []v1.ResourceName
	}{
		"nvidia":                  {pod: podRequesting(gpus(map[string]string{"nvidia.com/gpu": "1"})), want: []v1.ResourceName{"nvidia.com/gpu"}},
		"amd":                     {pod: podRequesting(gpus(map[string]string{"amd.com/gpu": "2"})), want: []v1.ResourceName{"amd.com/gpu"}},
		"both, in a stable order": {pod: podRequesting(gpus(map[string]string{"nvidia.com/gpu": "1", "amd.com/gpu": "1"})), want: []v1.ResourceName{"amd.com/gpu", "nvidia.com/gpu"}},
		"init container only": {
			pod:  podRequesting(gpus(map[string]string{"cpu": "1"}), gpus(map[string]string{"nvidia.com/gpu": "2"})),
			want: []v1.ResourceName{"nvidia.com/gpu"},
		},
		"zero request":                      {pod: podRequesting(gpus(map[string]string{"nvidia.com/gpu": "0"})), want: nil},
		"cpu only":                          {pod: podRequesting(gpus(map[string]string{"cpu": "1"})), want: nil},
		"extended resource that is not GPU": {pod: podRequesting(gpus(map[string]string{"example.com/foo": "1"})), want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, requestedGpuResources(test.pod))
		})
	}
}

func TestGpuResourceWithoutDeviceSource(t *testing.T) {
	mappedToDRA := func(resourceNames ...string) *extendedresourcecache.ExtendedResourceCache {
		cache := extendedresourcecache.NewExtendedResourceCache(klog.Background())
		for _, resourceName := range resourceNames {
			cache.OnAdd(&resourceapi.DeviceClass{
				ObjectMeta: metav1.ObjectMeta{Name: "class-" + resourceName},
				Spec:       resourceapi.DeviceClassSpec{ExtendedResourceName: ptr.To(resourceName)},
			}, false)
		}
		return cache
	}
	node := func(allocatable map[string]string) *node_info.NodeInfo {
		return &node_info.NodeInfo{Name: "node0", HasDRAGPUs: true, Node: &v1.Node{Status: v1.NodeStatus{Allocatable: gpus(allocatable)}}}
	}
	nvidia, amd := v1.ResourceName("nvidia.com/gpu"), v1.ResourceName("amd.com/gpu")

	for name, test := range map[string]struct {
		requested []v1.ResourceName
		node      *node_info.NodeInfo
		dbc       *extendedresourcecache.ExtendedResourceCache
		want      v1.ResourceName
	}{
		"nvidia on a DRA-only node, no mapping": {requested: []v1.ResourceName{nvidia}, node: node(nil), dbc: mappedToDRA(), want: nvidia},
		"amd on a DRA-only node, no mapping":    {requested: []v1.ResourceName{amd}, node: node(nil), dbc: mappedToDRA(), want: amd},
		"nil extended resource cache":           {requested: []v1.ResourceName{nvidia}, node: node(nil), dbc: nil, want: nvidia},
		"nvidia mapped to DRA":                  {requested: []v1.ResourceName{nvidia}, node: node(nil), dbc: mappedToDRA("nvidia.com/gpu")},
		"amd mapped to DRA":                     {requested: []v1.ResourceName{amd}, node: node(nil), dbc: mappedToDRA("amd.com/gpu")},
		"amd mapped, nvidia not, pod requests amd": {
			requested: []v1.ResourceName{amd}, node: node(nil), dbc: mappedToDRA("amd.com/gpu"),
		},
		"node also reports device plugin GPUs":   {requested: []v1.ResourceName{nvidia}, node: node(map[string]string{"nvidia.com/gpu": "4"}), dbc: mappedToDRA()},
		"node reports zero device plugin GPUs":   {requested: []v1.ResourceName{nvidia}, node: node(map[string]string{"nvidia.com/gpu": "0"}), dbc: mappedToDRA(), want: nvidia},
		"mixed vendors, only nvidia is reported": {requested: []v1.ResourceName{amd, nvidia}, node: node(map[string]string{"nvidia.com/gpu": "4"}), dbc: mappedToDRA(), want: amd},
		"mixed vendors, both are served": {
			requested: []v1.ResourceName{amd, nvidia}, node: node(map[string]string{"nvidia.com/gpu": "4"}), dbc: mappedToDRA("amd.com/gpu"),
		},
		"no GPU requested": {requested: nil, node: node(nil), dbc: mappedToDRA()},
	} {
		t.Run(name, func(t *testing.T) {
			got, found := gpuResourceWithoutDeviceSource(test.requested, test.node, test.dbc)
			assert.Equal(t, test.want != "", found)
			assert.Equal(t, test.want, got)
		})
	}
}
