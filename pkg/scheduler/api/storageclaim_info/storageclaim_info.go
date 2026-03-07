// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package storageclaim_info

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
)

type Key common_info.ObjectKey

type StorageClaimInfo struct {
	Key          Key
	Name         string
	Namespace    string
	Size         *resource.Quantity
	Phase        v1.PersistentVolumeClaimPhase
	StorageClass common_info.StorageClassID

	// PodOwnerReference is set only when there is a single pod owner to the PVC
	PodOwnerReference *PodOwnerReference
	DeletedOwner      bool
}

type PodOwnerReference struct {
	PodID        common_info.PodID
	PodName      string
	PodNamespace string
}

func (po *PodOwnerReference) Clone() *PodOwnerReference {
	return &PodOwnerReference{
		PodID:        po.PodID,
		PodName:      po.PodName,
		PodNamespace: po.PodNamespace,
	}
}

func NewKey(namespace, name string) Key {
	return Key(common_info.NewObjectKey(namespace, name))
}

func NewKeyFromClaim(claim *v1.PersistentVolumeClaim) Key {
	return NewKey(claim.Namespace, claim.Name)
}

func NewStorageClaimInfo(claim *v1.PersistentVolumeClaim, podOwner *PodOwnerReference) *StorageClaimInfo {
	return &StorageClaimInfo{
		Key:               NewKeyFromClaim(claim),
		Name:              claim.Name,
		Namespace:         claim.Namespace,
		Size:              claim.Spec.Resources.Requests.Storage(),
		Phase:             claim.Status.Phase,
		StorageClass:      common_info.StorageClassID(*claim.Spec.StorageClassName),
		PodOwnerReference: podOwner,
		DeletedOwner:      podOwner != nil,
	}
}

func (sc *StorageClaimInfo) Clone() *StorageClaimInfo {
	size := sc.Size.DeepCopy()

	var podOwner *PodOwnerReference
	if sc.PodOwnerReference != nil {
		podOwner = sc.PodOwnerReference.Clone()
	}

	return &StorageClaimInfo{
		Key:               sc.Key,
		Name:              sc.Name,
		Namespace:         sc.Namespace,
		Size:              &size,
		Phase:             sc.Phase,
		StorageClass:      sc.StorageClass,
		PodOwnerReference: podOwner,
	}
}

func (sc *StorageClaimInfo) MarkOwnerAlive() {
	sc.DeletedOwner = false
}

func (sc *StorageClaimInfo) HasDeletedOwner() bool {
	return sc.DeletedOwner
}

func GetPodOwner(claim *v1.PersistentVolumeClaim) (*PodOwnerReference, error) {
	if len(claim.OwnerReferences) > 1 {
		return nil, fmt.Errorf("multiple owners found for pvc %s/%s: %+v", claim.Namespace, claim.Name, claim.OwnerReferences)
	}

	if len(claim.OwnerReferences) == 0 {
		return nil, nil
	}

	if strings.ToLower(claim.OwnerReferences[0].Kind) != "pod" {
		return nil, nil
	}

	return &PodOwnerReference{
		PodID:        common_info.PodID(claim.OwnerReferences[0].UID),
		PodName:      claim.OwnerReferences[0].Name,
		PodNamespace: claim.Namespace,
	}, nil
}
