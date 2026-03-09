// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package storageclaim_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

func TestNewStorageClaimInfo(t *testing.T) {
	pvc := &v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-name",
			Namespace: "test-namespace",
			UID:       "pvc-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "pod",
					Name:       "pod-owner",
					UID:        "pod-uid",
				},
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			Resources: v1.VolumeResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					"storage": *resource.NewQuantity(10, resource.BinarySI),
				},
			},
			StorageClassName: pointer.String("storageclass-name"),
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: v1.ClaimBound,
		},
	}
	podOwner, err := GetPodOwner(pvc)
	assert.Nil(t, err)

	claimInfo := NewStorageClaimInfo(pvc, podOwner)
	assert.Equal(t, NewKey("test-namespace", "pvc-name"), claimInfo.Key)
	assert.Equal(t, "pvc-name", claimInfo.Name)
	assert.Equal(t, "test-namespace", claimInfo.Namespace)
	assert.Equal(t, 0, claimInfo.Size.Cmp(*resource.NewQuantity(10, resource.BinarySI)))
	assert.Equal(t, v1.ClaimBound, claimInfo.Phase)
	assert.Equal(t, common_info.StorageClassID("storageclass-name"), claimInfo.StorageClass)
	assert.Equal(t, common_info.PodID("pod-uid"), claimInfo.PodOwnerReference.PodID)
	assert.Equal(t, "pod-owner", claimInfo.PodOwnerReference.PodName)
	assert.Equal(t, "test-namespace", claimInfo.PodOwnerReference.PodNamespace)
}

func TestPodOwner(t *testing.T) {
	pvc := &v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-name",
			Namespace: "test-namespace",
			UID:       "pvc-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "not-pod",
					Name:       "pod-owner",
					UID:        "pod-uid",
				},
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			Resources: v1.VolumeResourceRequirements{
				Requests: map[v1.ResourceName]resource.Quantity{
					"storage": *resource.NewQuantity(10, resource.BinarySI),
				},
			},
			StorageClassName: pointer.String("storageclass-name"),
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: v1.ClaimBound,
		},
	}
	podOwner, err := GetPodOwner(pvc)
	assert.Nil(t, err)
	assert.Nil(t, podOwner)

	claimInfo := NewStorageClaimInfo(pvc, podOwner)
	assert.Equal(t, NewKey("test-namespace", "pvc-name"), claimInfo.Key)
	assert.Equal(t, "pvc-name", claimInfo.Name)
	assert.Equal(t, "test-namespace", claimInfo.Namespace)
	assert.Equal(t, 0, claimInfo.Size.Cmp(*resource.NewQuantity(10, resource.BinarySI)))
	assert.Equal(t, v1.ClaimBound, claimInfo.Phase)
	assert.Equal(t, common_info.StorageClassID("storageclass-name"), claimInfo.StorageClass)
	assert.Nil(t, claimInfo.PodOwnerReference)

	pvc.OwnerReferences = append(pvc.OwnerReferences, metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "pod",
		Name:       "bla",
		UID:        "bla",
	})

	podOwner, err = GetPodOwner(pvc)
	assert.Nil(t, podOwner)
	assert.NotNil(t, err)

	pvc.OwnerReferences = make([]metav1.OwnerReference, 0)
	podOwner, err = GetPodOwner(pvc)
	assert.Nil(t, podOwner)
	assert.Nil(t, err)
}
