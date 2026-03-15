// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package node_info

import (
	"fmt"
	"reflect"
	"testing"

	. "go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_affinity"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
)

type AddRemovePodsTestWithStorage struct {
	name                      string
	node                      *v1.Node
	pods                      []*pod_info.PodInfo
	rmPods                    []*pod_info.PodInfo
	expected                  *NodeInfo
	storageCapacities         map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo
	expectedStorageCapacities map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo
	vectorMap                 *resource_info.ResourceVectorMap
}

func capacityInfoEqual(l, r *storagecapacity_info.StorageCapacityInfo) bool {
	return reflect.DeepEqual(l, r)
}

func RunAddRemovePodsWithStorageTests(t *testing.T, tests []AddRemovePodsTestWithStorage) {
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := NewController(t)
			nodePodAffinityInfo := pod_affinity.NewMockNodePodAffinityInfo(controller)
			nodePodAffinityInfo.EXPECT().AddPod(Any()).Times(len(test.pods))
			nodePodAffinityInfo.EXPECT().RemovePod(Any()).Times(len(test.rmPods))

			test.vectorMap.AddResourceList(test.node.Status.Allocatable)
			ni := NewNodeInfo(test.node, nodePodAffinityInfo, test.vectorMap)

			for _, capacity := range test.storageCapacities {
				if capacity.IsNodeValid(ni.Node.Labels) {
					ni.AccessibleStorageCapacities[capacity.StorageClass] = append(ni.AccessibleStorageCapacities[capacity.StorageClass], capacity)
				}
			}

			for _, pod := range test.pods {
				_ = ni.AddTask(pod)
			}

			for _, pod := range test.rmPods {
				_ = ni.RemoveTask(pod)
			}

			setNodeInfoVectors(test.expected, test.vectorMap)

			var errors []error
			if !nodeInfoEqual(t, test.expected, ni) {
				errors = append(errors, fmt.Errorf("node info %d: \n expected %v, \n got %v \n",
					i, test.expected, ni))
			}

			for id, capacity := range test.storageCapacities {
				expectedCapacity, found := test.expectedStorageCapacities[id]
				if !found {
					errors = append(errors, fmt.Errorf("capacity info %s found on node but wasn't expected\n", id))
				}
				if !capacityInfoEqual(capacity, expectedCapacity) {
					errors = append(errors, fmt.Errorf("capacity info %s:  \n expected %v, \n got %v \n", id, expectedCapacity, capacity))
				}
			}

			if errors != nil {
				t.Errorf("error in test %s: %v", test.name, errors)
			}
		})
	}
}

func TestNodeInfoStorage_AddPod(t *testing.T) {
	namespace := "test-namespace"
	storageClassName := "storage-class"

	node1 := common_info.BuildNode("n1", common_info.BuildResourceList("8000m", "10G"))
	podAnnotations := map[string]string{
		pod_info.ReceivedResourceTypeAnnotationName: string(pod_info.ReceivedTypeRegular),
		commonconstants.PodGroupAnnotationForPod:    common_info.FakePogGroupId,
	}
	pod1 := common_info.BuildPod(namespace, "p1", "n1", v1.PodRunning,
		common_info.BuildResourceList("1000m", "1G"), []metav1.OwnerReference{},
		make(map[string]string), podAnnotations)
	pod2 := common_info.BuildPod(namespace, "p2", "n1", v1.PodRunning,
		common_info.BuildResourceList("2000m", "2G"), []metav1.OwnerReference{},
		make(map[string]string), podAnnotations)

	vectorMap := resource_info.NewResourceVectorMap()
	pod1Info := pod_info.NewTaskInfo(pod1, nil, vectorMap)
	pod2Info := pod_info.NewTaskInfo(pod2, nil, vectorMap)

	storageCapacityRaw := common_info.BuildStorageCapacity("capacity-name", namespace, storageClassName, 100, 100, nil)
	storageCapacity, _ := storagecapacity_info.NewStorageCapacityInfo(storageCapacityRaw)
	expectedStorageCapacity := storageCapacity.Clone()

	node1ExpectedNodeInfo := &NodeInfo{
		Name:                   "n1",
		Node:                   node1,
		Idle:                   common_info.BuildResourceWithGpu("7000m", "9G", "0", "109"),
		Used:                   common_info.BuildResourceWithGpu("1000m", "1G", "0", "1"),
		Releasing:              resource_info.EmptyResource(),
		Allocatable:            common_info.BuildResourceWithGpu("8000m", "10G", "0", "110"),
		PodInfos:               map[common_info.PodID]*pod_info.PodInfo{},
		LegacyMIGTasks:         map[common_info.PodID]string{},
		MemoryOfEveryGpuOnNode: DefaultGpuMemory,
		GpuSharingNodeInfo:     *newGpuSharingNodeInfo(),
		MaxTaskNum:             110,
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{
			storageCapacity.StorageClass: {expectedStorageCapacity},
		},
	}
	node1ExpectedNodeInfo.setAcceptedResources(pod1Info)
	node1ExpectedNodeInfo.PodInfos[pod1Info.UID] = pod1Info.Clone()

	tests := []AddRemovePodsTestWithStorage{
		{
			name:     "add 2 running non-owner pod",
			node:     node1,
			pods:     []*pod_info.PodInfo{pod1Info, pod2Info},
			rmPods:   []*pod_info.PodInfo{pod2Info},
			expected: node1ExpectedNodeInfo,
			storageCapacities: map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{
				storageCapacity.UID: storageCapacity,
			},
			expectedStorageCapacities: map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{
				storageCapacity.UID: storageCapacity.Clone(),
			},
			vectorMap: vectorMap,
		},
	}

	RunAddRemovePodsWithStorageTests(t, tests)
}

func TestAddTaskStorage(t *testing.T) {
	testNamespace := "test"
	storageClass := common_info.StorageClassID("storage-class")
	storageClaim := &storageclaim_info.StorageClaimInfo{
		Key:               storageclaim_info.NewKey(testNamespace, "pvc-name"),
		Name:              "pvc-name",
		Namespace:         testNamespace,
		Size:              resource.NewQuantity(10, resource.BinarySI),
		Phase:             v1.ClaimPending,
		StorageClass:      storageClass,
		PodOwnerReference: nil,
	}
	storageCapacity := &storagecapacity_info.StorageCapacityInfo{
		UID:             "storage-capacity",
		Name:            "",
		StorageClass:    storageClass,
		ProvisionedPVCs: map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
	}
	podInfo := pod_info.NewTaskInfo(&v1.Pod{}, nil, resource_info.NewResourceVectorMap())
	podInfo.UpsertStorageClaim(storageClaim)
	nodeInfo := &NodeInfo{
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{
			storageClass: {
				storageCapacity,
			},
		},
	}

	assert.Len(t, nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs, 0)

	nodeInfo.addTaskStorage(podInfo)
	assert.Equal(t, storageClaim.Clone(), nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs[storageClaim.Key])
	assert.Len(t, nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs, 1)

	nodeInfo.removeTaskStorage(podInfo)
	assert.Len(t, nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs, 0)

	// Verify that we don't add bound claims
	boundClaim := &storageclaim_info.StorageClaimInfo{
		Key:               storageclaim_info.NewKey(testNamespace, "bound-pvc-name"),
		Name:              "bound-pvc-name",
		Namespace:         testNamespace,
		Size:              resource.NewQuantity(10, resource.BinarySI),
		Phase:             v1.ClaimBound,
		StorageClass:      storageClass,
		PodOwnerReference: nil,
	}
	podInfo.UpsertStorageClaim(boundClaim)

	nodeInfo.addTaskStorage(podInfo)
	assert.Equal(t, storageClaim.Clone(), nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs[storageClaim.Key])
	assert.Len(t, nodeInfo.AccessibleStorageCapacities[storageClass][0].ProvisionedPVCs, 1)
}

func TestIsTaskStorageAllocatableMissingStorageclass(t *testing.T) {
	testNamespace := "test"
	storageClass := common_info.StorageClassID("storage-class")
	storageClaim := &storageclaim_info.StorageClaimInfo{
		Key:               storageclaim_info.NewKey(testNamespace, "pvc-name"),
		Name:              "pvc-name",
		Namespace:         testNamespace,
		Size:              resource.NewQuantity(10, resource.BinarySI),
		Phase:             v1.ClaimPending,
		StorageClass:      storageClass,
		PodOwnerReference: nil,
	}
	storageCapacityRaw := common_info.BuildStorageCapacity("capacity-name", testNamespace, string(storageClass), 100, 100, nil)
	storageCapacity, _ := storagecapacity_info.NewStorageCapacityInfo(storageCapacityRaw)

	podInfo := pod_info.NewTaskInfo(&v1.Pod{}, nil, resource_info.NewResourceVectorMap())
	podInfo.UpsertStorageClaim(storageClaim)
	nodeInfo := &NodeInfo{
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{
			storageClass: {
				storageCapacity,
			},
		},
	}

	allocatable, err := nodeInfo.isTaskStorageAllocatable(podInfo)
	assert.Nil(t, err)
	assert.True(t, allocatable)

	allocatable, err = nodeInfo.isTaskStorageAllocatableOnReleasingOrIdle(podInfo)
	assert.Nil(t, err)
	assert.True(t, allocatable)

	storageClaim.StorageClass = "wrong-storage-class"
	allocatable, err = nodeInfo.isTaskStorageAllocatable(podInfo)
	assert.Error(t, err)
	assert.False(t, allocatable)

	allocatable, err = nodeInfo.isTaskStorageAllocatableOnReleasingOrIdle(podInfo)
	assert.Error(t, err)
	assert.False(t, allocatable)
}

func TestIsTaskStorageAllocatableStorageCapacity(t *testing.T) {
	testNamespace := "test"
	storageClass := common_info.StorageClassID("storage-class")
	storageClaim := &storageclaim_info.StorageClaimInfo{
		Key:               storageclaim_info.NewKey(testNamespace, "pvc-name"),
		Name:              "pvc-name",
		Namespace:         testNamespace,
		Size:              resource.NewQuantity(10, resource.BinarySI),
		Phase:             v1.ClaimPending,
		StorageClass:      storageClass,
		PodOwnerReference: nil,
	}
	storageCapacityRaw := common_info.BuildStorageCapacity("capacity-name", testNamespace, string(storageClass), 100, 100, nil)
	storageCapacity, _ := storagecapacity_info.NewStorageCapacityInfo(storageCapacityRaw)

	podInfo := pod_info.NewTaskInfo(&v1.Pod{}, nil, resource_info.NewResourceVectorMap())
	podInfo.UpsertStorageClaim(storageClaim)
	nodeInfo := &NodeInfo{
		AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{
			storageClass: {
				storageCapacity,
			},
		},
	}

	allocatable, err := nodeInfo.isTaskStorageAllocatable(podInfo)
	assert.Nil(t, err)
	assert.True(t, allocatable)

	allocatable, err = nodeInfo.isTaskStorageAllocatableOnReleasingOrIdle(podInfo)
	assert.Nil(t, err)
	assert.True(t, allocatable)

	storageClaim.Size = resource.NewQuantity(110, resource.BinarySI)
	allocatable, err = nodeInfo.isTaskStorageAllocatable(podInfo)
	assert.Error(t, err)
	assert.False(t, allocatable)

	allocatable, err = nodeInfo.isTaskStorageAllocatableOnReleasingOrIdle(podInfo)
	assert.Error(t, err)
	assert.False(t, allocatable)
}
