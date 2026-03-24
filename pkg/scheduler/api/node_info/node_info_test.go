/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	. "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
)

const (
	MibToMbScale       = 1.048576
	migEnabledLabelKey = "node-role.kubernetes.io/mig-enabled"
)

func nodeInfoEqual(t *testing.T, result, expected *NodeInfo) bool {
	expected.PodAffinityInfo = nil
	result.PodAffinityInfo = nil
	expected.MaxTaskNum = 0
	result.MaxTaskNum = 0
	expected.GpuMemorySynced = false
	result.GpuMemorySynced = false

	equal := true
	if expected.Name != result.Name {
		t.Logf("Name differs: expected %q, got %q", expected.Name, result.Name)
		equal = false
	}
	if !reflect.DeepEqual(expected.Node, result.Node) {
		t.Logf("Node differs: expected %v, got %v", expected.Node, result.Node)
		equal = false
	}
	if !reflect.DeepEqual(expected.AllocatableVector, result.AllocatableVector) {
		t.Logf("AllocatableVector differs: expected %v, got %v", expected.AllocatableVector, result.AllocatableVector)
		equal = false
	}
	if !reflect.DeepEqual(expected.IdleVector, result.IdleVector) {
		t.Logf("IdleVector differs: expected %v, got %v", expected.IdleVector, result.IdleVector)
		equal = false
	}
	if !reflect.DeepEqual(expected.UsedVector, result.UsedVector) {
		t.Logf("UsedVector differs: expected %v, got %v", expected.UsedVector, result.UsedVector)
		equal = false
	}
	if !reflect.DeepEqual(expected.ReleasingVector, result.ReleasingVector) {
		t.Logf("ReleasingVector differs: expected %v, got %v", expected.ReleasingVector, result.ReleasingVector)
		equal = false
	}
	if !reflect.DeepEqual(expected.VectorMap, result.VectorMap) {
		t.Logf("VectorMap differs: expected %v, got %v", expected.VectorMap, result.VectorMap)
		equal = false
	}
	if !reflect.DeepEqual(expected.PodInfos, result.PodInfos) {
		t.Logf("PodInfos differs: expected %d pods, got %d pods", len(expected.PodInfos), len(result.PodInfos))
		for k, v := range expected.PodInfos {
			if rv, ok := result.PodInfos[k]; !ok {
				t.Logf("  PodInfos[%s]: missing in got", k)
			} else if !reflect.DeepEqual(v, rv) {
				t.Logf("  PodInfos[%s]: differs", k)
			}
		}
		for k := range result.PodInfos {
			if _, ok := expected.PodInfos[k]; !ok {
				t.Logf("  PodInfos[%s]: unexpected in got", k)
			}
		}
		equal = false
	}
	if !reflect.DeepEqual(expected.LegacyMIGTasks, result.LegacyMIGTasks) {
		t.Logf("LegacyMIGTasks differs: expected %v, got %v", expected.LegacyMIGTasks, result.LegacyMIGTasks)
		equal = false
	}
	if expected.MemoryOfEveryGpuOnNode != result.MemoryOfEveryGpuOnNode {
		t.Logf("MemoryOfEveryGpuOnNode differs: expected %v, got %v", expected.MemoryOfEveryGpuOnNode, result.MemoryOfEveryGpuOnNode)
		equal = false
	}
	if !reflect.DeepEqual(expected.GpuSharingNodeInfo, result.GpuSharingNodeInfo) {
		t.Logf("GpuSharingNodeInfo differs: expected %+v, got %+v", expected.GpuSharingNodeInfo, result.GpuSharingNodeInfo)
		equal = false
	}
	if !reflect.DeepEqual(expected.AccessibleStorageCapacities, result.AccessibleStorageCapacities) {
		t.Logf("AccessibleStorageCapacities differs: expected %v, got %v", expected.AccessibleStorageCapacities, result.AccessibleStorageCapacities)
		equal = false
	}
	if expected.HasDRAGPUs != result.HasDRAGPUs {
		t.Logf("HasDRAGPUs differs: expected %v, got %v", expected.HasDRAGPUs, result.HasDRAGPUs)
		equal = false
	}
	return equal
}

func testVectorMapFromNode(node *v1.Node) *resource_info.ResourceVectorMap {
	vectorMap := resource_info.NewResourceVectorMap()
	for resourceName := range node.Status.Allocatable {
		vectorMap.AddResource(resourceName)
	}
	return vectorMap
}

func nodeInfoEqualExplainable(l, r *NodeInfo) error {
	var errors []error

	if l.Name != r.Name {
		errors = append(errors, fmt.Errorf("Name: exp %v, got %v", l.Name, r.Name))
	}
	if !reflect.DeepEqual(l.Node, r.Node) {
		errors = append(errors, fmt.Errorf("Node: exp %v, got %v\ndiff:\n%s", l.Node, r.Node, cmp.Diff(l.Node, r.Node)))
	}
	if !reflect.DeepEqual(l.ReleasingVector, r.ReleasingVector) {
		errors = append(errors, fmt.Errorf("ReleasingVector: exp %v, got %v", l.ReleasingVector, r.ReleasingVector))
	}
	if !reflect.DeepEqual(l.IdleVector, r.IdleVector) {
		errors = append(errors, fmt.Errorf("IdleVector: exp %v, got %v", l.IdleVector, r.IdleVector))
	}
	if !reflect.DeepEqual(l.UsedVector, r.UsedVector) {
		errors = append(errors, fmt.Errorf("UsedVector: exp %v, got %v", l.UsedVector, r.UsedVector))
	}
	if !reflect.DeepEqual(l.AllocatableVector, r.AllocatableVector) {
		errors = append(errors, fmt.Errorf("AllocatableVector: exp %v, got %v", l.AllocatableVector, r.AllocatableVector))
	}
	if !reflect.DeepEqual(l.AccessibleStorageCapacities, r.AccessibleStorageCapacities) {
		errors = append(errors, fmt.Errorf("AccessibleStorageCapacities: exp %v, got %v", l.AccessibleStorageCapacities, r.AccessibleStorageCapacities))
	}
	if !reflect.DeepEqual(l.PodInfos, r.PodInfos) {
		errors = append(errors, fmt.Errorf("PodInfos: exp %v, got %v", l.PodInfos, r.PodInfos))
	}
	if l.MaxTaskNum != r.MaxTaskNum {
		errors = append(errors, fmt.Errorf("MaxTaskNum: exp %v, got %v", l.MaxTaskNum, r.MaxTaskNum))
	}
	if l.MemoryOfEveryGpuOnNode != r.MemoryOfEveryGpuOnNode {
		errors = append(errors, fmt.Errorf("MemoryOfEveryGpuOnNode: exp %v, got %v", l.MemoryOfEveryGpuOnNode, r.MemoryOfEveryGpuOnNode))
	}
	if l.GpuMemorySynced != r.GpuMemorySynced {
		errors = append(errors, fmt.Errorf("GpuMemorySynced: exp %v, got %v", l.GpuMemorySynced, r.GpuMemorySynced))
	}
	if !reflect.DeepEqual(l.LegacyMIGTasks, r.LegacyMIGTasks) {
		errors = append(errors, fmt.Errorf("LegacyMIGTasks: exp %v, got %v", l.LegacyMIGTasks, r.LegacyMIGTasks))
	}
	if l.HasDRAGPUs != r.HasDRAGPUs {
		errors = append(errors, fmt.Errorf("HasDRAGPUs: exp %v, got %v", l.HasDRAGPUs, r.HasDRAGPUs))
	}
	if !reflect.DeepEqual(l.GpuSharingNodeInfo, r.GpuSharingNodeInfo) {
		errors = append(errors, fmt.Errorf("GpuSharingNodeInfo: exp %v, got %v", l.GpuSharingNodeInfo, r.GpuSharingNodeInfo))
	}
	if len(errors) > 0 {
		return fmt.Errorf("node info: \n%v", errors)
	}
	if !reflect.DeepEqual(l, r) {
		errors = append(errors, fmt.Errorf("unknown difference: exp %v, got %v", l, r))
	}
	return nil
}

type AddRemovePodsTest struct {
	name     string
	node     *v1.Node
	pods     []*v1.Pod
	rmPods   []*v1.Pod
	expected *NodeInfo
	// Resource values for computing expected vectors (vectorMap isn't available at struct-literal time)
	expectedIdle        *resource_info.Resource
	expectedUsed        *resource_info.Resource
	expectedReleasing   *resource_info.Resource
	expectedAllocatable *resource_info.Resource
}

type podCreationOptions struct {
	GPUs      float64
	releasing bool
	gpuGroup  string
}

func RunAddRemovePodsTests(t *testing.T, tests []AddRemovePodsTest) {
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := NewController(t)
			nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinityInfo.EXPECT().AddPod(Any()).Times(len(test.pods))
			nodePodAffinityInfo.EXPECT().RemovePod(Any()).Times(len(test.rmPods))

			vectorMap := testVectorMapFromNode(test.node)
			for _, pod := range append(test.pods, test.rmPods...) {
				for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
					vectorMap.AddResourceList(container.Resources.Requests)
				}
			}

			ni := NewNodeInfo(test.node, nodePodAffinityInfo, vectorMap)

			for _, pod := range test.pods {
				_ = ni.AddTask(pod_info.NewTaskInfo(pod, nil, vectorMap))
			}

			for _, pod := range test.rmPods {
				pi := pod_info.NewTaskInfo(pod, nil, vectorMap)
				_ = ni.RemoveTask(pi)
			}

			test.expected.VectorMap = vectorMap
			if test.expectedAllocatable != nil {
				test.expected.AllocatableVector = test.expectedAllocatable.ToVector(vectorMap)
			}
			if test.expectedIdle != nil {
				test.expected.IdleVector = test.expectedIdle.ToVector(vectorMap)
			}
			if test.expectedUsed != nil {
				test.expected.UsedVector = test.expectedUsed.ToVector(vectorMap)
			}
			if test.expectedReleasing != nil {
				test.expected.ReleasingVector = test.expectedReleasing.ToVector(vectorMap)
			}
			for podID, podInfo := range test.expected.PodInfos {
				podInfo.SetVectorMap(vectorMap)
				test.expected.PodInfos[podID] = podInfo
			}

			t.Log(test.expected.PodInfos)
			if !nodeInfoEqual(t, test.expected, ni) {
				t.Errorf("node info %d: \n expected %v, \n got %v \n",
					i, test.expected, ni)
			}
		})
	}
}

func TestNodeInfo_AddPod(t *testing.T) {
	node1 := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	podAnnotations := map[string]string{
		pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
		commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
	}
	pod1 := common_info.BuildPod("c1", "p1", "n1", v1.PodRunning,
		common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
		make(map[string]string), podAnnotations)
	pod2 := common_info.BuildPod("c1", "p2", "n1", v1.PodRunning,
		common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{},
		make(map[string]string), podAnnotations)

	node1ExpectedNodeInfo := &NodeInfo{
		Name: "n1",
		Node: node1,
		PodInfos: map[common_info.PodID]*pod_info.PodInfo{
			"c1/p1": pod_info.NewTaskInfo(pod1, nil, resource_info.NewResourceVectorMap()),
			"c1/p2": pod_info.NewTaskInfo(pod2, nil, resource_info.NewResourceVectorMap()),
		},
		LegacyMIGTasks:              map[common_info.PodID]string{},
		MemoryOfEveryGpuOnNode:      DefaultGpuMemory,
		GpuSharingNodeInfo:          *newGpuSharingNodeInfo(),
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
	}

	node1ExpectedNodeInfo.MaxTaskNum = 110
	for _, podInfo := range node1ExpectedNodeInfo.PodInfos {
		node1ExpectedNodeInfo.setAcceptedResources(podInfo)
	}

	tests := []AddRemovePodsTest{
		{
			name:     "add 2 running non-owner pod",
			node:     node1,
			pods:     []*v1.Pod{pod1, pod2},
			expected: node1ExpectedNodeInfo,
			expectedIdle: func() *resource_info.Resource {
				r := common_info.BuildResource("5000m", "7G")
				r.ScalarResources()[resource_info.PodsResourceName] = 108
				return r
			}(),
			expectedUsed: func() *resource_info.Resource {
				r := common_info.BuildResource("3000m", "3G")
				r.ScalarResources()[resource_info.PodsResourceName] = 2
				return r
			}(),
			expectedReleasing: resource_info.EmptyResource(),
			expectedAllocatable: func() *resource_info.Resource {
				r := common_info.BuildResource("8000m", "10G")
				r.ScalarResources()[resource_info.PodsResourceName] = 110
				return r
			}(),
		},
	}

	RunAddRemovePodsTests(t, tests)
}

func TestNodeInfo_RemovePod(t *testing.T) {
	node1 := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))

	podAnnotations := map[string]string{
		pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
		commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
	}

	pod1 := common_info.BuildPod("c1", "p1", "n1", v1.PodRunning, common_info.BuildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{}, make(map[string]string), podAnnotations)
	pod2 := common_info.BuildPod("c1", "p2", "n1", v1.PodRunning, common_info.BuildResourceList("2000m", "2G"),
		[]metav1.OwnerReference{}, make(map[string]string), podAnnotations)
	pod3 := common_info.BuildPod("c1", "p3", "n1", v1.PodRunning, common_info.BuildResourceList("3000m", "3G"),
		[]metav1.OwnerReference{}, make(map[string]string), podAnnotations)
	pod1PodInfo := pod_info.NewTaskInfo(pod1, nil, resource_info.NewResourceVectorMap())
	pod3PodInfo := pod_info.NewTaskInfo(pod3, nil, resource_info.NewResourceVectorMap())

	node1ExpectedNodeInfo := &NodeInfo{
		Name: "n1",
		Node: node1,
		PodInfos: map[common_info.PodID]*pod_info.PodInfo{
			"c1/p1": pod1PodInfo,
			"c1/p3": pod3PodInfo,
		},
		LegacyMIGTasks:              map[common_info.PodID]string{},
		MemoryOfEveryGpuOnNode:      DefaultGpuMemory,
		GpuSharingNodeInfo:          *newGpuSharingNodeInfo(),
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
	}
	node1ExpectedNodeInfo.MaxTaskNum = 110
	node1ExpectedNodeInfo.setAcceptedResources(pod1PodInfo)
	node1ExpectedNodeInfo.setAcceptedResources(pod3PodInfo)

	tests := []AddRemovePodsTest{
		{
			name:     "add 3 running non-owner pod, remove 1 running non-owner pod",
			node:     node1,
			pods:     []*v1.Pod{pod1, pod2, pod3},
			rmPods:   []*v1.Pod{pod2},
			expected: node1ExpectedNodeInfo,
			expectedIdle: func() *resource_info.Resource {
				r := common_info.BuildResource("4000m", "6G")
				r.ScalarResources()[resource_info.PodsResourceName] = 108
				return r
			}(),
			expectedUsed: func() *resource_info.Resource {
				r := common_info.BuildResource("4000m", "4G")
				r.ScalarResources()[resource_info.PodsResourceName] = 2
				return r
			}(),
			expectedReleasing: resource_info.EmptyResource(),
			expectedAllocatable: func() *resource_info.Resource {
				r := common_info.BuildResource("8000m", "10G")
				r.ScalarResources()[resource_info.PodsResourceName] = 110
				return r
			}(),
		},
	}

	RunAddRemovePodsTests(t, tests)
}

func TestAddRemovePods(t *testing.T) {
	type podInfoMetadata struct {
		pod       *v1.Pod
		status    pod_status.PodStatus
		gpuGroups []string
	}
	type expectedResources struct {
		idle        *resource_info.Resource
		used        *resource_info.Resource
		releasing   *resource_info.Resource
		allocatable *resource_info.Resource
	}
	type addRemovePodsTestData struct {
		name                     string
		node                     *v1.Node
		podsInfoMetadata         []podInfoMetadata
		addedPodsNodeInfo        *NodeInfo
		removedPodsNodeInfo      *NodeInfo
		addedExpectedResources   expectedResources
		removedExpectedResources expectedResources
	}

	tests := []addRemovePodsTestData{
		{
			name: "releasing pod",
			node: common_info.BuildNode("n1",
				common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "1", "110")),
			podsInfoMetadata: []podInfoMetadata{
				{
					pod: common_info.BuildPod("c1", "p1", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "0.5"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Releasing,
					gpuGroups: []string{"1"},
				},
			},
			addedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUs["1"] = true
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 50
					sharingMaps.UsedSharedGPUsMemory["1"] = 50
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 50
					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			addedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("7000m", "9G", "0", "109"),
				used:        common_info.BuildResourceWithGpu("1000m", "1G", "0", "1"),
				releasing:   common_info.BuildResourceWithGpu("1000m", "1G", "1", "1"),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
			removedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 0
					sharingMaps.UsedSharedGPUsMemory["1"] = 0
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 0
					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			removedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
				used:        resource_info.EmptyResource(),
				releasing:   resource_info.EmptyResource(),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
		},
		{
			name: "pipelined pod - different gpus",
			node: common_info.BuildNode("n1",
				common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "1", "110")),
			podsInfoMetadata: []podInfoMetadata{
				{
					pod: common_info.BuildPod("c1", "p1", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "0.5"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Releasing,
					gpuGroups: []string{"1"},
				},
				{
					pod: common_info.BuildPod("c1", "p2", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("500m", "1G", "0.5"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Pipelined,
					gpuGroups: []string{"2"},
				},
			},
			addedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUs["1"] = true
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 50
					sharingMaps.ReleasingSharedGPUsMemory["2"] = -50
					sharingMaps.UsedSharedGPUsMemory["1"] = 50
					sharingMaps.UsedSharedGPUsMemory["2"] = 50
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 50

					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			addedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("7000m", "9G", "0", "109"),
				used:        common_info.BuildResourceWithGpu("1500m", "2G", "0", "2"),
				releasing:   common_info.BuildResourceWithGpuNoPods("500m", "0G", "0"),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
			removedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 0
					sharingMaps.ReleasingSharedGPUsMemory["2"] = 0
					sharingMaps.UsedSharedGPUsMemory["1"] = 0
					sharingMaps.UsedSharedGPUsMemory["2"] = 0
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 0
					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			removedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
				used:        resource_info.EmptyResource(),
				releasing:   resource_info.EmptyResource(),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
		},
		{
			name: "pipelined pod - same gpus",
			node: common_info.BuildNode("n1",
				common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "1", "110")),
			podsInfoMetadata: []podInfoMetadata{
				{
					pod: common_info.BuildPod("c1", "p1", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "0.5"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Releasing,
					gpuGroups: []string{"1"},
				},
				{
					pod: common_info.BuildPod("c1", "p2", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "0.2"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Running,
					gpuGroups: []string{"1"},
				},
				{
					pod: common_info.BuildPod("c1", "p3", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("500m", "1G", "0.5"), []metav1.OwnerReference{},
						make(map[string]string), map[string]string{
							pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
							commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
						}),
					status:    pod_status.Pipelined,
					gpuGroups: []string{"1"},
				},
			},
			addedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 0
					sharingMaps.UsedSharedGPUsMemory["1"] = 120
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 70
					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			addedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("6000m", "8G", "0", "108"),
				used:        common_info.BuildResourceWithGpu("2500m", "3G", "0", "3"),
				releasing:   common_info.BuildResourceWithGpuNoPods("500m", "0G", "0"),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
			removedPodsNodeInfo: &NodeInfo{
				Name:                   "n1",
				PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
				LegacyMIGTasks:         map[common_info.PodID]string{},
				MemoryOfEveryGpuOnNode: DefaultGpuMemory,
				GpuSharingNodeInfo: func() GpuSharingNodeInfo {
					sharingMaps := *newGpuSharingNodeInfo()
					sharingMaps.ReleasingSharedGPUsMemory["1"] = 0
					sharingMaps.UsedSharedGPUsMemory["1"] = 0
					sharingMaps.AllocatedSharedGPUsMemory["1"] = 0
					return sharingMaps
				}(),
				AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
			},
			removedExpectedResources: expectedResources{
				idle:        common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
				used:        resource_info.EmptyResource(),
				releasing:   resource_info.EmptyResource(),
				allocatable: common_info.BuildResourceWithGpu("8000m", "10G", "1", "110"),
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test %d: %s", i, test.name)

			test.addedPodsNodeInfo.Node = test.node
			test.removedPodsNodeInfo.Node = test.node

			test.addedPodsNodeInfo.MaxTaskNum = 110
			test.removedPodsNodeInfo.MaxTaskNum = 110

			controller := NewController(t)
			nodePodAffinityInfoAdded := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinityInfoAdded.EXPECT().AddPod(Any()).Times(len(test.podsInfoMetadata))

			vectorMap := testVectorMapFromNode(test.node)
			for _, podInfoMetaData := range test.podsInfoMetadata {
				for _, container := range append(podInfoMetaData.pod.Spec.InitContainers, podInfoMetaData.pod.Spec.Containers...) {
					vectorMap.AddResourceList(container.Resources.Requests)
				}
			}
			setExpectedVectors := func(ni *NodeInfo, res expectedResources) {
				ni.VectorMap = vectorMap
				if res.allocatable != nil {
					ni.AllocatableVector = res.allocatable.ToVector(vectorMap)
				}
				if res.idle != nil {
					ni.IdleVector = res.idle.ToVector(vectorMap)
				}
				if res.used != nil {
					ni.UsedVector = res.used.ToVector(vectorMap)
				}
				if res.releasing != nil {
					ni.ReleasingVector = res.releasing.ToVector(vectorMap)
				}
			}
			setExpectedVectors(test.addedPodsNodeInfo, test.addedExpectedResources)
			setExpectedVectors(test.removedPodsNodeInfo, test.removedExpectedResources)
			ni := NewNodeInfo(test.node, nodePodAffinityInfoAdded, vectorMap)

			var podsInfo []*pod_info.PodInfo
			for _, podInfoMetaData := range test.podsInfoMetadata {
				pi := pod_info.NewTaskInfo(podInfoMetaData.pod, nil, vectorMap)
				pi.Status = podInfoMetaData.status
				pi.GPUGroups = podInfoMetaData.gpuGroups
				podsInfo = append(podsInfo, pi)
			}

			for _, podInfo := range podsInfo {
				_ = ni.AddTask(podInfo)
				podInfoKey := common_info.PodID(fmt.Sprintf("%s/%s", podInfo.Namespace, podInfo.Name))
				test.addedPodsNodeInfo.PodInfos[podInfoKey] = podInfo.Clone()
			}
			if err := nodeInfoEqualExplainable(ni, test.addedPodsNodeInfo); err != nil {
				t.Errorf("Diff in node info after adding pods: \n%v", err)
			}

			nodePodAffinityInfoRemoved := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinityInfoRemoved.EXPECT().RemovePod(Any()).Times(len(test.podsInfoMetadata))
			ni.PodAffinityInfo = nodePodAffinityInfoRemoved

			// This is an important line - the remove code might not work if it won't remove in reverse order
			// Because the statement discard in reverse order to operations addition, it should be fine.
			slices.Reverse(podsInfo)

			for _, podInfo := range podsInfo {
				_ = ni.RemoveTask(podInfo)
			}
			if err := nodeInfoEqualExplainable(ni, test.removedPodsNodeInfo); err != nil {
				t.Errorf("Diff in node info after removing pods: \n%v", err)
			}
		})
	}
}

type allocatableTestData struct {
	node                    *v1.Node
	podsResources           []v1.ResourceList
	podResourcesToAllocate  v1.ResourceList
	podAnnotations          map[string]string
	podOverhead             v1.ResourceList
	expected                bool
	expectedMessageContains []string
	expectOverheadMessage   bool
}
type allocatableTestFunction func(ni *NodeInfo, task *pod_info.PodInfo) (bool, *common_info.TasksFitError)

func TestIsTaskAllocatable(t *testing.T) {
	nodeCapacityDifferent := common_info.BuildNode("n2", common_info.BuildResourceList("1000m", "1G"))
	nodeCapacityDifferent.Status.Capacity = common_info.BuildResourceList("2000m", "2G")

	tests := map[string]allocatableTestData{
		"add pod with not enough cpu and memory": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("2000m", "2G"),
			expected:                false,
			expectedMessageContains: []string{"CPU cores", "memory"},
		},
		"add pod with not enough cpu - 1 millicpu": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1001m", "1G"),
			expected:                false,
			expectedMessageContains: []string{"CPU cores"},
		},
		"add pod with not enough memory - 1 Kb": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "2G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1000m", "1Ki"),
			expected:                false,
			expectedMessageContains: []string{"memory"},
		},
		"add pod with enough cpu and memory": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1000m", "1G"),
			expected:                true,
			expectedMessageContains: []string{},
		},
		"task in capacity but not in available": {
			node:                    nodeCapacityDifferent,
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1000m", "1G"),
			expected:                false,
			expectedMessageContains: []string{"CPU cores", "memory"},
		},
		"missing gpu": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{},
			podResourcesToAllocate:  common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
			expected:                false,
			expectedMessageContains: []string{"GPU"},
		},
		"missing gpu - requesting a fraction": {
			node:          common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources: []v1.ResourceList{},
			// the build pod function will convert it to annotation similarly to the admission controller
			podResourcesToAllocate:  common_info.BuildResourceListWithGPU("1000m", "1G", "500m"),
			expected:                false,
			expectedMessageContains: []string{"GPU"},
		},
		"already used gpu so missing gpu": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("2000m", "2G", "1", "110")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceListWithGPU("1000m", "1G", "1")},
			podResourcesToAllocate:  common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
			expected:                false,
			expectedMessageContains: []string{"GPU"},
		},
		"enough cpu memory and gpu": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("2000m", "2G", "2", "110")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceListWithGPU("1000m", "1G", "1")},
			podResourcesToAllocate:  common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
			expected:                true,
			expectedMessageContains: []string{},
		},
		"pod with overhead that fits without overhead but not with overhead": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("500m", "500M"),
			podOverhead:             common_info.BuildResourceList("600m", "600M"),
			expected:                false,
			expectedMessageContains: []string{"CPU cores", "memory"},
			expectOverheadMessage:   true,
		},
		"pod with overhead that doesn't fit even without overhead": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1500m", "1500M"),
			podOverhead:             common_info.BuildResourceList("100m", "100M"),
			expected:                false,
			expectedMessageContains: []string{"CPU cores", "memory"},
			expectOverheadMessage:   false,
		},
		"pod without overhead that doesn't fit": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("1500m", "1500M"),
			podOverhead:             v1.ResourceList{},
			expected:                false,
			expectedMessageContains: []string{"CPU cores", "memory"},
			expectOverheadMessage:   false,
		},
		"pod with overhead that fits with overhead": {
			node:                    common_info.BuildNode("n1", common_info.BuildResourceList("2000m", "2G")),
			podsResources:           []v1.ResourceList{common_info.BuildResourceList("1000m", "1G")},
			podResourcesToAllocate:  common_info.BuildResourceList("500m", "500M"),
			podOverhead:             common_info.BuildResourceList("100m", "100M"),
			expected:                true,
			expectedMessageContains: []string{},
			expectOverheadMessage:   false,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			runAllocatableTest(
				t, testData, testName,
				func(ni *NodeInfo, task *pod_info.PodInfo) (bool, *common_info.TasksFitError) {
					return ni.IsTaskAllocatable(task), ni.FittingError(task, false)
				},
			)
		})
	}
}

func TestIsTaskAllocatableOnReleasingOrIdle(t *testing.T) {
	singleMigNode := common_info.BuildNode("single-mig", common_info.BuildResourceListWithGPUAndPods("2000m", "2G", "8", "110"))
	singleMigNode.Labels[migEnabledLabelKey] = "true"

	mixedMigNode := common_info.BuildNode("mixed-mig", common_info.BuildResourceListWithGPUAndPods("2000m", "2G", "8", "110"))
	mixedMigNode.Labels[migEnabledLabelKey] = "true"
	mixedMigNode.Labels[commonconstants.GpuCountLabel] = "8"
	mixedMigNode.Labels[GpuMemoryLabel] = "40"

	tests := map[string]allocatableTestData{
		"cpu only job on single mig node with enough resources": {
			node:                   singleMigNode,
			podsResources:          []v1.ResourceList{common_info.BuildResourceList("200m", "500m")},
			podResourcesToAllocate: common_info.BuildResourceList("200m", "500m"),
			expected:               true,
		},
		"cpu only job on mixed mig node with enough resources": {
			node:                   mixedMigNode,
			podsResources:          []v1.ResourceList{common_info.BuildResourceList("200m", "500m")},
			podResourcesToAllocate: common_info.BuildResourceList("200m", "500m"),
			expected:               true,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			runAllocatableTest(
				t, testData, testName,
				func(ni *NodeInfo, task *pod_info.PodInfo) (bool, *common_info.TasksFitError) {
					return ni.IsTaskAllocatableOnReleasingOrIdle(task), nil
				},
			)
		})
	}
}

func runAllocatableTest(
	t *testing.T, testData allocatableTestData, testName string,
	testedFunction allocatableTestFunction,
) {
	controller := NewController(t)
	nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
	nodePodAffinityInfo.EXPECT().AddPod(Any()).Times(len(testData.podsResources))

	vectorMap := testVectorMapFromNode(testData.node)
	for _, podResources := range testData.podsResources {
		for resourceName := range podResources {
			vectorMap.AddResource(resourceName)
		}
	}
	for resourceName := range testData.podResourcesToAllocate {
		vectorMap.AddResource(resourceName)
	}
	ni := NewNodeInfo(testData.node, nodePodAffinityInfo, vectorMap)
	for ind, podResouces := range testData.podsResources {
		pod := common_info.BuildPod(
			fmt.Sprintf("p%d", ind), "p1", "n1", v1.PodRunning, podResouces,
			[]metav1.OwnerReference{}, make(map[string]string), map[string]string{})
		addJobAnnotation(pod)
		pi := pod_info.NewTaskInfo(pod, nil, vectorMap)
		if err := ni.AddTask(pi); err != nil {
			t.Errorf("%s: failed to add pod %v, index: %d", testName, pi, ind)
		}
	}
	pod := common_info.BuildPod(
		"podToAllocate", "p1", "n1", v1.PodRunning, testData.podResourcesToAllocate,
		[]metav1.OwnerReference{}, make(map[string]string), testData.podAnnotations)
	addJobAnnotation(pod)

	if len(testData.podOverhead) > 0 {
		pod.Spec.Overhead = testData.podOverhead
	}

	task := pod_info.NewTaskInfo(pod, nil, vectorMap)
	allocatable, fitErr := testedFunction(ni, task)
	if allocatable != testData.expected {
		t.Errorf("%s: is pod allocatable: expected %v, got %v", testName, testData.expected, allocatable)
	}
	if fitErr != nil {
		for _, expectedMessage := range testData.expectedMessageContains {
			if !strings.Contains(fitErr.Error(), expectedMessage) {
				t.Errorf("%s: expected error message to contain %s, got %s", testName, expectedMessage, fitErr.Error())
			}
		}

		if testData.expectOverheadMessage {
			if !strings.Contains(fitErr.Error(), "Not enough resources due to pod overhead resources") {
				t.Errorf("%s: expected overhead message, got %s", testName, fitErr.Error())
			}
		} else {
			fitErrMessage := fitErr.Error()
			if strings.Contains(fitErrMessage, "Not enough resources due to pod overhead resources") {
				t.Errorf("%s: unexpected overhead message, got %s", testName, fitErrMessage)
			}
		}
	} else if len(testData.expectedMessageContains) > 0 {
		// If we expected an error but got none, that's a test failure
		t.Errorf("%s: expected error but got none", testName)
	}
}

func TestGpuOperatorHasMemoryError_MibInput(t *testing.T) {
	testNode := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	testNode.Labels[GpuMemoryLabel] = "4096"
	gpuMemoryInMb, ok := getNodeGpuMemory(testNode)
	assert.Equal(t, true, ok)
	assert.Equal(t, int64(4000), gpuMemoryInMb)
}

func TestGpuOperatorHasMemoryError_Bytes(t *testing.T) {
	testNode := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	testNode.Labels[GpuMemoryLabel] = "4295000001"
	gpuMemoryInMb, ok := getNodeGpuMemory(testNode)
	assert.Equal(t, true, ok)
	assert.Equal(t, int64(4000), gpuMemoryInMb)
}

func addJobAnnotation(pod *v1.Pod) {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[commonconstants.PodGroupAnnotationForPod] = pod.Name
}

func TestNodeInfo_isTaskAllocatableOnNonAllocatedResources(t *testing.T) {
	type fields struct {
		Name                   string
		Node                   *v1.Node
		Releasing              *resource_info.Resource
		Idle                   *resource_info.Resource
		Used                   *resource_info.Resource
		Allocatable            *resource_info.Resource
		PodInfos               map[common_info.PodID]*pod_info.PodInfo
		MaxTaskNum             int
		MemoryOfEveryGpuOnNode int64
		GpuMemorySynced        bool
		PodAffinityInfo        pod_affinity.NodePodAffinityInfo
		GpuSharingNodeInfo     GpuSharingNodeInfo
	}
	type args struct {
		task                      *pod_info.PodInfo
		nodeNonAllocatedResources *resource_info.Resource
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			"invalid fraction from memory request",
			fields{
				MemoryOfEveryGpuOnNode: 1000,
				GpuMemorySynced:        true,
				Idle: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
				Used:      resource_info.EmptyResource(),
				Releasing: resource_info.EmptyResource(),
				Allocatable: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
			},
			args{
				task: pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "p1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
								pod_info.GpuMemoryAnnotationName:         "1500",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "c1",
								},
							},
						},
					}, nil, resource_info.NewResourceVectorMap(),
				),
				nodeNonAllocatedResources: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
			},
			false,
		},
		{
			"valid fraction from memory request",
			fields{
				MemoryOfEveryGpuOnNode: 1000,
				GpuMemorySynced:        true,
				Idle: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
				Used:      resource_info.EmptyResource(),
				Releasing: resource_info.EmptyResource(),
				Allocatable: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
			},
			args{
				task: pod_info.NewTaskInfo(
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "p1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
								pod_info.GpuMemoryAnnotationName:         "1000",
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "c1",
								},
							},
						},
					}, nil, resource_info.NewResourceVectorMap(),
				),
				nodeNonAllocatedResources: func() *resource_info.Resource {
					r := resource_info.NewResource(0, 0, 2)
					r.ScalarResources()[resource_info.PodsResourceName] = 10
					return r
				}(),
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			ni := &NodeInfo{
				Name:                   tt.fields.Name,
				Node:                   tt.fields.Node,
				VectorMap:              vectorMap,
				PodInfos:               tt.fields.PodInfos,
				MaxTaskNum:             tt.fields.MaxTaskNum,
				MemoryOfEveryGpuOnNode: tt.fields.MemoryOfEveryGpuOnNode,
				GpuMemorySynced:        tt.fields.GpuMemorySynced,
				PodAffinityInfo:        tt.fields.PodAffinityInfo,
				GpuSharingNodeInfo:     tt.fields.GpuSharingNodeInfo,
			}
			if tt.fields.Allocatable != nil {
				ni.AllocatableVector = tt.fields.Allocatable.ToVector(vectorMap)
			}
			if tt.fields.Idle != nil {
				ni.IdleVector = tt.fields.Idle.ToVector(vectorMap)
			}
			if tt.fields.Used != nil {
				ni.UsedVector = tt.fields.Used.ToVector(vectorMap)
			}
			if tt.fields.Releasing != nil {
				ni.ReleasingVector = tt.fields.Releasing.ToVector(vectorMap)
			}
			nodeNonAllocatedVector := tt.args.nodeNonAllocatedResources.ToVector(vectorMap)
			assert.Equalf(t, tt.want,
				ni.isTaskAllocatableOnNonAllocatedResources(tt.args.task, nodeNonAllocatedVector),
				"isTaskAllocatableOnNonAllocatedResources(%v, %v)", tt.args.task, tt.args.nodeNonAllocatedResources)
		})
	}
}

func TestNodeInfo_GetSumOfIdleGPUs(t *testing.T) {
	tests := []struct {
		name           string
		capacity       map[v1.ResourceName]resource.Quantity
		allocatable    map[v1.ResourceName]resource.Quantity
		tasks          []*pod_info.PodInfo
		expectedGPUs   float64
		expectedMemory int64
	}{
		{
			name:           "no tasks",
			capacity:       common_info.BuildResourceListWithGPU("8000m", "10G", "2"),
			allocatable:    common_info.BuildResourceListWithGPU("8000m", "10G", "2"),
			tasks:          []*pod_info.PodInfo{},
			expectedGPUs:   2,
			expectedMemory: 200,
		},
		{
			name:        "whole GPU tasks",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 2}),
				createPod("team-a", "pod2", podCreationOptions{GPUs: 1}),
			},
			expectedGPUs:   5,
			expectedMemory: 500,
		},
		{
			name:        "fraction GPU tasks on different devices",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 0.5, gpuGroup: "group1"}),
				createPod("team-a", "pod2", podCreationOptions{GPUs: 0.1, gpuGroup: "group2"}),
			},
			expectedGPUs:   7.4,
			expectedMemory: 740,
		},
		{
			name:        "fraction GPU tasks on same device",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 0.5, gpuGroup: "group1"}),
				createPod("team-a", "pod2", podCreationOptions{GPUs: 0.1, gpuGroup: "group1"}),
			},
			expectedGPUs:   7.4,
			expectedMemory: 740,
		},
		{
			name:        "mixed task types",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 2}),
				createPod("team-a", "pod2", podCreationOptions{GPUs: 0.5, gpuGroup: "group1"}),
				createPod("team-a", "pod3", podCreationOptions{GPUs: 1}),
				createPod("team-a", "pod4", podCreationOptions{GPUs: 0.1, gpuGroup: "group2"}),
			},
			expectedGPUs:   4.4,
			expectedMemory: 440,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Capacity:    tt.capacity,
					Allocatable: tt.allocatable,
				},
			}

			controller := NewController(t)
			nodePodAffinity := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinity.EXPECT().AddPod(Any()).Times(len(tt.tasks))

			ni := NewNodeInfo(node, nodePodAffinity, testVectorMapFromNode(node))
			for _, task := range tt.tasks {
				assert.Nil(t, ni.AddTask(task))
			}
			idleGPUs, idleMemory := ni.GetSumOfIdleGPUs()
			assert.Equalf(t, tt.expectedGPUs, idleGPUs,
				"test: %s, expected GPU %f, got %f", tt.name, tt.expectedGPUs, idleGPUs)
			assert.Equalf(t, tt.expectedMemory, idleMemory,
				"test: %s, expected GPU memory %d, got %d", tt.name, tt.expectedMemory, idleMemory)
		})
	}
}

func TestNodeInfo_GetSumOfReleasingGPUs(t *testing.T) {
	tests := []struct {
		name           string
		capacity       map[v1.ResourceName]resource.Quantity
		allocatable    map[v1.ResourceName]resource.Quantity
		tasks          []*pod_info.PodInfo
		expectedGPUs   float64
		expectedMemory int64
	}{
		{
			name:           "no tasks",
			capacity:       common_info.BuildResourceListWithGPU("8000m", "10G", "2"),
			allocatable:    common_info.BuildResourceListWithGPU("8000m", "10G", "2"),
			tasks:          []*pod_info.PodInfo{},
			expectedGPUs:   0,
			expectedMemory: 0,
		},
		{
			name:        "whole GPU tasks",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 2, releasing: true}),
				createPod("team-a", "pod2", podCreationOptions{GPUs: 1, releasing: true}),
			},
			expectedGPUs:   3,
			expectedMemory: 300,
		},
		{
			name:        "fraction GPU tasks on different devices",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1",
					podCreationOptions{GPUs: 0.5, releasing: true, gpuGroup: "group1"}),
				createPod("team-a", "pod2",
					podCreationOptions{GPUs: 0.1, releasing: true, gpuGroup: "group2"}),
			},
			expectedGPUs:   2,
			expectedMemory: 200,
		},
		{
			name:        "fraction GPU tasks on same device",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1",
					podCreationOptions{GPUs: 0.5, releasing: true, gpuGroup: "group1"}),
				createPod("team-a", "pod2",
					podCreationOptions{GPUs: 0.1, releasing: true, gpuGroup: "group1"}),
			},
			expectedGPUs:   1,
			expectedMemory: 100,
		},
		{
			name:        "mixed task types",
			capacity:    common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			allocatable: common_info.BuildResourceListWithGPU("8000m", "10G", "8"),
			tasks: []*pod_info.PodInfo{
				createPod("team-a", "pod1", podCreationOptions{GPUs: 2, releasing: true}),
				createPod("team-a", "pod2",
					podCreationOptions{GPUs: 0.5, releasing: true, gpuGroup: "group1"}),
				createPod("team-a", "pod3", podCreationOptions{GPUs: 1, releasing: true}),
			},
			expectedGPUs:   4,
			expectedMemory: 400,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: v1.NodeStatus{
					Capacity:    tt.capacity,
					Allocatable: tt.allocatable,
				},
			}

			controller := NewController(t)
			nodePodAffinity := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinity.EXPECT().AddPod(Any()).Times(len(tt.tasks))

			ni := NewNodeInfo(node, nodePodAffinity, testVectorMapFromNode(node))
			for _, task := range tt.tasks {
				assert.Nil(t, ni.AddTask(task))
			}
			idleGPUs, idleMemory := ni.GetSumOfReleasingGPUs()
			assert.Equalf(t, tt.expectedGPUs, idleGPUs,
				"test: %s, expected GPU %f, got %f", tt.name, tt.expectedGPUs, idleGPUs)
			assert.Equalf(t, tt.expectedMemory, idleMemory,
				"test: %s, expected GPU memory %d, got %d", tt.name, tt.expectedMemory, idleMemory)
		})
	}
}

func createPod(namespace, name string, options podCreationOptions) *pod_info.PodInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "pg1",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node1",
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{},
						Limits:   v1.ResourceList{},
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}

	if options.releasing {
		pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	}

	numGPUsStr := strconv.FormatFloat(options.GPUs, 'f', -1, 64)
	if options.GPUs >= 1 {
		pod.Spec.Containers[0].Resources.Requests[resource_info.GPUResourceName] = resource.MustParse(numGPUsStr)
		pod.Spec.Containers[0].Resources.Limits[resource_info.GPUResourceName] = resource.MustParse(numGPUsStr)
	} else {
		pod.Annotations[commonconstants.GpuFraction] = numGPUsStr
	}

	task := pod_info.NewTaskInfo(pod, nil, resource_info.NewResourceVectorMap())
	task.GPUGroups = []string{options.gpuGroup}
	return task
}

func Test_isMigResource(t *testing.T) {
	tests := []struct {
		name  string
		rName string
		want  bool
	}{
		{
			"is mig",
			"nvidia.com/mig-1g.5gb",
			true,
		},
		{
			"not mig",
			"nvidia.com/gpu",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMigResource(v1.ResourceName(tt.rName)); got != tt.want {
				t.Errorf("isMigResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPredicateByNodeResourcesType_DRA(t *testing.T) {
	tests := map[string]struct {
		nodeInfo    *NodeInfo
		allocatable *resource_info.Resource
		task        *pod_info.PodInfo
		expectError bool
		errorMsg    string
	}{
		"Device-plugin GPU request on DRA-only node": {
			nodeInfo: &NodeInfo{
				Name:       "dra-node",
				HasDRAGPUs: true,
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "dra-node", Labels: map[string]string{}},
				},
			},
			allocatable: common_info.BuildResourceWithGpu("1000m", "1G", "4", "110"),
			task:        createPod("default", "gpu-pod", podCreationOptions{GPUs: 1}),
			expectError: true,
			errorMsg:    "device-plugin GPU requests cannot be scheduled on DRA-only nodes",
		},
		"CPU-only request on DRA-only node": {
			nodeInfo: &NodeInfo{
				Name:       "dra-node",
				HasDRAGPUs: true,
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "dra-node", Labels: map[string]string{}},
				},
			},
			allocatable: common_info.BuildResourceWithGpu("1000m", "1G", "4", "110"),
			task:        createPod("default", "cpu-pod", podCreationOptions{GPUs: 0}),
			expectError: false,
			errorMsg:    "",
		},
		"Device-plugin GPU request on device-plugin node": {
			nodeInfo: &NodeInfo{
				Name:       "device-plugin-node",
				HasDRAGPUs: false,
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "device-plugin-node", Labels: map[string]string{}},
				},
			},
			allocatable: common_info.BuildResourceWithGpu("1000m", "1G", "4", "110"),
			task:        createPod("default", "gpu-pod", podCreationOptions{GPUs: 1}),
			expectError: false,
			errorMsg:    "",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			testData.nodeInfo.VectorMap = vectorMap
			if testData.allocatable != nil {
				testData.nodeInfo.AllocatableVector = testData.allocatable.ToVector(vectorMap)
			}
			err := testData.nodeInfo.PredicateByNodeResourcesType(testData.task)
			if testData.expectError {
				assert.Error(t, err, "Should reject request")
				assert.Contains(t, err.Error(), testData.errorMsg)
			} else {
				assert.NoError(t, err, "Should allow request")
			}
		})
	}
}

func TestIsCPUOnlyNode_DRA(t *testing.T) {
	vectorMap := resource_info.NewResourceVectorMap()
	nodeWithDRA := &NodeInfo{
		Name:              "dra-node",
		HasDRAGPUs:        true,
		AllocatableVector: common_info.BuildResourceWithGpu("1000m", "1G", "4", "110").ToVector(vectorMap),
		VectorMap:         vectorMap,
		Node:              &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "dra-node", Labels: map[string]string{}}},
	}
	assert.False(t, nodeWithDRA.IsCPUOnlyNode(), "node with HasDRAGPUs should not be CPU-only")

	cpuOnlyNode := &NodeInfo{
		Name:              "cpu-node",
		HasDRAGPUs:        false,
		AllocatableVector: common_info.BuildResource("1000m", "1G").ToVector(vectorMap),
		VectorMap:         vectorMap,
		Node:              &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cpu-node", Labels: map[string]string{}}},
	}
	assert.True(t, cpuOnlyNode.IsCPUOnlyNode(), "node without GPUs and without HasDRAGPUs should be CPU-only")
}

func TestResourceReservationPodConsumesMaxPods(t *testing.T) {
	tests := []struct {
		name              string
		node              *v1.Node
		pods              []*v1.Pod
		expectedIdle      *resource_info.Resource
		expectedUsed      *resource_info.Resource
		expectedReleasing *resource_info.Resource
	}{
		{
			name: "resource reservation pod consumes pod slot, CPU, memory but not GPU",
			node: common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "2", "110")),
			pods: []*v1.Pod{
				func() *v1.Pod {
					pod := common_info.BuildPod("c1", "reservation-pod", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
						[]metav1.OwnerReference{}, map[string]string{
							commonconstants.AppLabelName: "kai-resource-reservation",
						}, map[string]string{
							commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
						})
					return pod
				}(),
			},
			expectedIdle: func() *resource_info.Resource {
				// Idle should have: original - (cpu, memory, pods) but GPU unchanged
				r := common_info.BuildResourceWithGpu("7000m", "9G", "2", "109")
				return r
			}(),
			expectedUsed: func() *resource_info.Resource {
				// Used should have: cpu, memory, pods but NOT GPU
				r := common_info.BuildResource("1000m", "1G")
				r.ScalarResources()[resource_info.PodsResourceName] = 1
				return r
			}(),
			expectedReleasing: resource_info.EmptyResource(),
		},
		{
			name: "mixed reservation and regular pods",
			node: common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "2", "110")),
			pods: []*v1.Pod{
				func() *v1.Pod {
					pod := common_info.BuildPod("c1", "reservation-pod", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
						[]metav1.OwnerReference{}, map[string]string{
							commonconstants.AppLabelName: "kai-resource-reservation",
						}, map[string]string{
							commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
						})
					return pod
				}(),
				common_info.BuildPod("c1", "regular-pod", "n1", v1.PodRunning,
					common_info.BuildResourceListWithGPU("2000m", "2G", "1"),
					[]metav1.OwnerReference{}, make(map[string]string), map[string]string{
						pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
						commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
					}),
			},
			expectedIdle: func() *resource_info.Resource {
				// Reservation pod: 1000m CPU, 1G memory, 0 GPU, 1 pod
				// Regular pod: 2000m CPU, 2G memory, 1 GPU, 1 pod
				// Idle = 8000m - 3000m CPU, 10G - 3G memory, 2 - 1 GPU, 110 - 2 pods
				r := common_info.BuildResourceWithGpu("5000m", "7G", "1", "108")
				return r
			}(),
			expectedUsed: func() *resource_info.Resource {
				// Reservation pod: 1000m CPU, 1G memory, 0 GPU, 1 pod
				// Regular pod: 2000m CPU, 2G memory, 1 GPU, 1 pod
				// Total: 3000m CPU, 3G memory, 1 GPU, 2 pods
				r := common_info.BuildResourceWithGpu("3000m", "3G", "1", "2")
				return r
			}(),
			expectedReleasing: resource_info.EmptyResource(),
		},
		{
			name: "releasing resource reservation pod",
			node: common_info.BuildNode("n1", common_info.BuildResourceListWithGPUAndPods("8000m", "10G", "2", "110")),
			pods: []*v1.Pod{
				func() *v1.Pod {
					pod := common_info.BuildPod("c1", "reservation-pod", "n1", v1.PodRunning,
						common_info.BuildResourceListWithGPU("1000m", "1G", "1"),
						[]metav1.OwnerReference{}, map[string]string{
							commonconstants.AppLabelName: "kai-resource-reservation",
						}, map[string]string{
							commonconstants.PodGroupAnnotationForPod: common_info.FakePogGroupId,
						})
					pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					return pod
				}(),
			},
			expectedIdle: func() *resource_info.Resource {
				// Idle should have: original - (cpu, memory, pods) but GPU unchanged
				r := common_info.BuildResourceWithGpu("7000m", "9G", "2", "109")
				return r
			}(),
			expectedUsed: func() *resource_info.Resource {
				// Used should have: cpu, memory, pods but NOT GPU
				r := common_info.BuildResource("1000m", "1G")
				r.ScalarResources()[resource_info.PodsResourceName] = 1
				return r
			}(),
			expectedReleasing: func() *resource_info.Resource {
				// Releasing should have: cpu, memory, pods but NOT GPU
				r := common_info.BuildResource("1000m", "1G")
				r.ScalarResources()[resource_info.PodsResourceName] = 1
				return r
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := NewController(t)
			nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinityInfo.EXPECT().AddPod(Any()).Times(len(tt.pods))

			vectorMap := resource_info.NewResourceVectorMap()
			vectorMap.AddResourceList(tt.node.Status.Allocatable)
			ni := NewNodeInfo(tt.node, nodePodAffinityInfo, vectorMap)

			for _, pod := range tt.pods {
				pi := pod_info.NewTaskInfo(pod, nil, vectorMap)
				err := ni.AddTask(pi)
				assert.NoError(t, err, "failed to add pod")
			}

			expectedIdleVector := tt.expectedIdle.ToVector(vectorMap)
			expectedUsedVector := tt.expectedUsed.ToVector(vectorMap)
			expectedReleasingVector := tt.expectedReleasing.ToVector(vectorMap)

			// Verify idle resources
			if !reflect.DeepEqual(ni.IdleVector, expectedIdleVector) {
				t.Errorf("IdleVector mismatch:\nexpected: %v\ngot: %v",
					expectedIdleVector, ni.IdleVector)
			}

			// Verify used resources
			if !reflect.DeepEqual(ni.UsedVector, expectedUsedVector) {
				t.Errorf("UsedVector mismatch:\nexpected: %v\ngot: %v",
					expectedUsedVector, ni.UsedVector)
			}

			// Verify releasing resources
			if !reflect.DeepEqual(ni.ReleasingVector, expectedReleasingVector) {
				t.Errorf("ReleasingVector mismatch:\nexpected: %v\ngot: %v",
					expectedReleasingVector, ni.ReleasingVector)
			}
		})
	}
}
