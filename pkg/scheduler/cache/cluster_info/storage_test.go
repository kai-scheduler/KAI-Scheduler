// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/storagecapacity_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/constants/status"
)

const (
	testNamespace      = "test-namespace"
	storageClass       = "storage-class"
	ownedClaimName     = "owned-pvc-name"
	nonOwnedClaimName  = "non-owned-pvc-name"
	unrelatedClaimName = "unrelated-pvc-name"
)

var (
	ownedClaimKey     = storageclaim_info.NewKey(testNamespace, ownedClaimName)
	nonOwnedClaimKey  = storageclaim_info.NewKey(testNamespace, nonOwnedClaimName)
	unrelatedClaimKey = storageclaim_info.NewKey(testNamespace, unrelatedClaimName)
)

func TestSetStorageObjects(t *testing.T) {
	storageClaims := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{
		ownedClaimKey: {
			Key:          ownedClaimKey,
			Name:         ownedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
			PodOwnerReference: &storageclaim_info.PodOwnerReference{
				PodID:        "owner-pod-id",
				PodName:      "owner-pod-name",
				PodNamespace: testNamespace,
			},
		},
	}

	storageCapacities := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{
		common_info.StorageCapacityID("storage-capacity-id"): {
			UID:               "storage-capacity-id",
			Name:              "storage-capacity",
			StorageClass:      storageClass,
			ProvisionedPVCs:   map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
			MaximumVolumeSize: resource.NewQuantity(100, resource.BinarySI),
		},
	}

	podInfo := pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "owner-pod-id",
			Name:      "owner-pod-name",
			Namespace: testNamespace,
		},
		Spec: v1.PodSpec{
			NodeName: "node-name",
			Volumes: []v1.Volume{
				{
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: ownedClaimName,
						},
					},
				},
			},
		},
		Status: v1.PodStatus{
			Phase: status.Running,
		},
	}, nil, resource_info.NewResourceVectorMap())

	existingPods := map[common_info.PodID]*pod_info.PodInfo{
		common_info.PodID("owner-pod-id"): podInfo,
	}

	nodes := map[string]*node_info.NodeInfo{
		"node-name": {
			Name: "node-name",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"labelKey": "labelValue",
				}},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
	}

	linkStorageObjects(storageClaims, storageCapacities, existingPods, nodes)

	assert.Equal(t, storageClaims[ownedClaimKey], existingPods["owner-pod-id"].GetOwnedStorageClaims()[ownedClaimKey])

	assert.Equal(t, 1, len(storageCapacities["storage-capacity-id"].ProvisionedPVCs))
	assert.Equal(t, storageClaims[ownedClaimKey], storageCapacities["storage-capacity-id"].ProvisionedPVCs[ownedClaimKey])

	assert.Equal(t, 1, len(nodes["node-name"].AccessibleStorageCapacities))
}

func TestSetStorageObjectsMultiplePVCs(t *testing.T) {
	storageClaims := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{
		ownedClaimKey: {
			Key:          ownedClaimKey,
			Name:         ownedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
			PodOwnerReference: &storageclaim_info.PodOwnerReference{
				PodID:        "owner-pod-id",
				PodName:      "owner-pod-name",
				PodNamespace: testNamespace,
			},
		},
		nonOwnedClaimKey: {
			Key:          nonOwnedClaimKey,
			Name:         nonOwnedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
		},
		unrelatedClaimKey: {
			Key:          unrelatedClaimKey,
			Name:         unrelatedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
		},
	}

	storageCapacities := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{
		common_info.StorageCapacityID("storage-capacity-id"): {
			UID:               "storage-capacity-id",
			Name:              "storage-capacity",
			StorageClass:      storageClass,
			ProvisionedPVCs:   map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
			MaximumVolumeSize: resource.NewQuantity(100, resource.BinarySI),
		},
	}

	existingPods := map[common_info.PodID]*pod_info.PodInfo{
		common_info.PodID("owner-pod-id"): pod_info.NewTaskInfo(
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "owner-pod-id",
					Name:      "owner-pod-name",
					Namespace: testNamespace,
				},
				Spec: v1.PodSpec{
					NodeName: "node-name",
					Volumes: []v1.Volume{
						{
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: ownedClaimName,
								},
							},
						},
						{
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: nonOwnedClaimName,
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Phase: status.Running,
				},
			}, nil, resource_info.NewResourceVectorMap()),
	}

	nodes := map[string]*node_info.NodeInfo{
		"node-name": {
			Name: "node-name",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"labelKey": "labelValue",
				}},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
	}

	linkStorageObjects(storageClaims, storageCapacities, existingPods, nodes)

	assert.Equal(t, storageClaims[ownedClaimKey], existingPods["owner-pod-id"].GetOwnedStorageClaims()[ownedClaimKey])
	assert.Equal(t, 1, len(existingPods["owner-pod-id"].GetOwnedStorageClaims()))

	assert.Equal(t, storageClaims[ownedClaimKey], existingPods["owner-pod-id"].GetAllStorageClaims()[ownedClaimKey])
	assert.Equal(t, storageClaims[nonOwnedClaimKey], existingPods["owner-pod-id"].GetAllStorageClaims()[nonOwnedClaimKey])
	assert.Equal(t, 2, len(existingPods["owner-pod-id"].GetAllStorageClaims()))

	assert.Equal(t, 2, len(storageCapacities["storage-capacity-id"].ProvisionedPVCs))
	assert.Equal(t, storageClaims[ownedClaimKey], storageCapacities["storage-capacity-id"].ProvisionedPVCs[ownedClaimKey])
	assert.Equal(t, storageClaims[nonOwnedClaimKey], storageCapacities["storage-capacity-id"].ProvisionedPVCs[nonOwnedClaimKey])

	assert.Equal(t, 1, len(nodes["node-name"].AccessibleStorageCapacities))
}

func TestSetStorageObjects_ReleaseOwnedPVCs(t *testing.T) {
	storageClaims := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{
		ownedClaimKey: {
			Key:          ownedClaimKey,
			Name:         ownedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
			PodOwnerReference: &storageclaim_info.PodOwnerReference{
				PodID:        "owner-pod-id",
				PodName:      "owner-pod-name",
				PodNamespace: testNamespace,
			},
		},
		nonOwnedClaimKey: {
			Key:          nonOwnedClaimKey,
			Name:         nonOwnedClaimName,
			Namespace:    testNamespace,
			Size:         resource.NewQuantity(10, resource.BinarySI),
			Phase:        v1.ClaimBound,
			StorageClass: storageClass,
		},
	}

	capacityRaw := v12.CSIStorageCapacity{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storage-capacity",
			UID:  "storage-capacity-id",
		},
		NodeTopology:      nil,
		StorageClassName:  storageClass,
		Capacity:          resource.NewQuantity(100, resource.BinarySI),
		MaximumVolumeSize: resource.NewQuantity(100, resource.BinarySI),
	}

	capacity, err := storagecapacity_info.NewStorageCapacityInfo(&capacityRaw)
	assert.Nil(t, err)

	storageCapacities := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{
		common_info.StorageCapacityID("storage-capacity-id"): capacity,
	}

	now := metav1.Now()
	existingPods := map[common_info.PodID]*pod_info.PodInfo{
		common_info.PodID("owner-pod-id"): pod_info.NewTaskInfo(
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:               "owner-pod-id",
					Name:              "owner-pod-name",
					Namespace:         testNamespace,
					DeletionTimestamp: &now,
				},
				Spec: v1.PodSpec{
					NodeName: "node-name",
					Volumes: []v1.Volume{
						{
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: ownedClaimName,
								},
							},
						},
						{
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: nonOwnedClaimName,
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Phase: status.Running,
				},
			}, nil, resource_info.NewResourceVectorMap()),
	}

	nodes := map[string]*node_info.NodeInfo{
		"node-name": {
			Name: "node-name",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"labelKey": "labelValue",
				}},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
	}

	linkStorageObjects(storageClaims, storageCapacities, existingPods, nodes)

	assert.Equal(t, v1.ClaimBound, storageClaims[ownedClaimKey].Phase)
	assert.Equal(t, v1.ClaimBound, storageClaims[nonOwnedClaimKey].Phase)

	allocatable := storageCapacities["storage-capacity-id"].Allocatable()
	assert.Equal(t, 0, allocatable.Cmp(*resource.NewQuantity(100, resource.BinarySI)))

	nodePodInfos := map[common_info.PodID]*pod_info.PodInfo{
		common_info.PodID(fmt.Sprintf("%s/%s", testNamespace, "owner-pod-name")): existingPods["owner-pod-id"],
	}

	releasing := storageCapacities["storage-capacity-id"].Releasing(nodePodInfos)
	assert.Equal(t, 0, releasing.Cmp(*storageClaims[ownedClaimKey].Size))
}

func TestSetNodesAccessibleCapacities(t *testing.T) {
	selector := metav1.LabelSelector{}
	metav1.AddLabelToSelector(&selector, "key", "value")

	rawCapacities := []v12.CSIStorageCapacity{
		{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "capacity-1",
				UID:  "capacity-1-uid",
			},
			NodeTopology:     &selector,
			StorageClassName: storageClass,
		},
	}
	capacities := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{}
	for _, capacity := range rawCapacities {
		capacityInfo, err := storagecapacity_info.NewStorageCapacityInfo(&capacity)
		assert.Nil(t, err)
		capacities[capacityInfo.UID] = capacityInfo
	}

	nodes := map[string]*node_info.NodeInfo{
		"node-1": {
			Name: "node-1",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"key": "value"},
				},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
		"node-2": {
			Name: "node-2",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
	}
	setNodesAccessibleCapacities(capacities, nodes)

	assert.Equal(t, 1, len(nodes["node-1"].AccessibleStorageCapacities[storageClass]))
	assert.Equal(t, 0, len(nodes["node-2"].AccessibleStorageCapacities[storageClass]))
}

func TestSetNodesAccessibleCapacities_DisableMultiCapacity(t *testing.T) {
	selector := metav1.LabelSelector{}
	metav1.AddLabelToSelector(&selector, "key", "value")

	rawCapacities := []v12.CSIStorageCapacity{
		{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "capacity-1",
				UID:  "capacity-1-uid",
			},
			NodeTopology:     &selector,
			StorageClassName: storageClass,
		},
		{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: "capacity-2",
				UID:  "capacity-2-uid",
			},
			StorageClassName: storageClass,
		},
	}
	capacities := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{}
	for _, capacity := range rawCapacities {
		capacityInfo, err := storagecapacity_info.NewStorageCapacityInfo(&capacity)
		assert.Nil(t, err)
		capacities[capacityInfo.UID] = capacityInfo
	}

	nodes := map[string]*node_info.NodeInfo{
		"node-1": {
			Name: "node-1",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"key": "value"},
				},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
		"node-2": {
			Name: "node-2",
			Node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			AccessibleStorageCapacities: map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{},
		},
	}
	setNodesAccessibleCapacities(capacities, nodes)

	assert.Equal(t, 0, len(nodes["node-1"].AccessibleStorageCapacities[storageClass]))
	assert.Equal(t, 1, len(nodes["node-2"].AccessibleStorageCapacities[storageClass]))
}
