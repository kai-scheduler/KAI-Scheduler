// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package storagecapacity_info

import (
	"fmt"

	v13 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
)

type StorageCapacityInfo struct {
	UID  common_info.StorageCapacityID
	Name string

	// StorageClass is the name of the StorageClass that this capacity applies to.
	StorageClass common_info.StorageClassID

	// ProvisionedPVCs is a map of all existing PVCs provisioned from a node with access to this capacity and
	// with matching StorageClass, and are reclaimable (owned by a single pod)
	ProvisionedPVCs map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo

	// The capacity on this storage reported by the csidriver
	capacity *resource.Quantity

	// MaximumVolumeSize is the largest size that may be used in a
	// ResourceRequirements.Requests in a volume claim that uses this storage capacity.
	MaximumVolumeSize *resource.Quantity

	nodeTopology labels.Selector
}

func NewStorageCapacityInfo(capacity *v1.CSIStorageCapacity) (*StorageCapacityInfo, error) {
	var selector labels.Selector
	selector = labels.Everything()

	if capacity.NodeTopology != nil {
		var err error
		selector, err = v12.LabelSelectorAsSelector(capacity.NodeTopology)
		if err != nil {
			return nil, err
		}
	}

	return &StorageCapacityInfo{
		UID:               common_info.StorageCapacityID(capacity.UID),
		Name:              capacity.Name,
		StorageClass:      common_info.StorageClassID(capacity.StorageClassName),
		ProvisionedPVCs:   map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{},
		capacity:          capacity.Capacity, // ToDo: get something more accurate
		MaximumVolumeSize: capacity.MaximumVolumeSize,
		nodeTopology:      selector,
	}, nil
}

func (sc *StorageCapacityInfo) Clone() *StorageCapacityInfo {
	provisionedPVCs := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{}

	for key, claim := range sc.ProvisionedPVCs {
		provisionedPVCs[key] = claim
	}

	capacity := sc.capacity.DeepCopy()
	maximumVolumeSize := sc.MaximumVolumeSize.DeepCopy()
	return &StorageCapacityInfo{
		UID:               sc.UID,
		Name:              sc.Name,
		StorageClass:      sc.StorageClass,
		ProvisionedPVCs:   provisionedPVCs,
		capacity:          &capacity,
		MaximumVolumeSize: &maximumVolumeSize,
		nodeTopology:      sc.nodeTopology.DeepCopySelector(),
	}
}

func (sc *StorageCapacityInfo) IsNodeValid(nodeLabels map[string]string) bool {
	if sc.nodeTopology == nil {
		return true
	}

	return sc.nodeTopology.Matches(labels.Set(nodeLabels))
}

// ArePVCsAllocatable returns true if all unbound PVCs passed can be allocated together by this storage capacity,
// considering storage capacity and MaximumVolumeSize
func (sc *StorageCapacityInfo) ArePVCsAllocatable(pvcs []*storageclaim_info.StorageClaimInfo) error {
	totalRequest := resource.NewQuantity(0, resource.BinarySI)

	for _, pvc := range pvcs {
		totalRequest.Add(*pvc.Size)
	}

	if totalRequest.Cmp(sc.Allocatable()) == 1 {
		allocatable := sc.Allocatable()
		return newPVCFitError(totalRequest, &allocatable, sc.Name)
	}

	return nil
}

// ArePVCsAllocatableOnReleasingOrIdle returns true if all unbound PVCs passed can be allocated together by this storage capacity,
// considering storage capacity, storage capacity of possibly releasing pvcs, and MaximumVolumeSize
func (sc *StorageCapacityInfo) ArePVCsAllocatableOnReleasingOrIdle(pvcs []*storageclaim_info.StorageClaimInfo, podInfos map[common_info.PodID]*pod_info.PodInfo) error {
	totalRequest := resource.NewQuantity(0, resource.BinarySI)

	for _, pvc := range pvcs {
		totalRequest.Add(*pvc.Size)
	}

	allocatableIncludingReleasing := sc.Allocatable()
	allocatableIncludingReleasing.Add(sc.Releasing(podInfos))
	if totalRequest.Cmp(allocatableIncludingReleasing) == 1 {
		return newPVCFitError(totalRequest, &allocatableIncludingReleasing, sc.Name)
	}

	return nil
}

// Allocatable calculates the allocatable storage for the capacity. Generally, we assume that the allocatable capacity
// stated in the object is the allocatable. However, if we have virtually allocated pvcs on this capacity, the pvc object
// is still considered PENDING and not taken into account in StorageCapacityInfo.capacity. So we subtract all claims that are PENDING.
func (sc *StorageCapacityInfo) Allocatable() resource.Quantity {
	if sc.capacity == nil {
		return *resource.NewQuantity(0, resource.BinarySI)
	}

	allocatable := sc.capacity.DeepCopy()
	for _, claim := range sc.ProvisionedPVCs {
		if claim.Phase == v13.ClaimBound {
			continue
		}
		allocatable.Sub(*claim.Size)
	}
	return allocatable
}

func (sc *StorageCapacityInfo) Releasing(podInfos map[common_info.PodID]*pod_info.PodInfo) resource.Quantity {
	releasing := resource.NewQuantity(0, resource.BinarySI)
	for _, claim := range sc.ProvisionedPVCs {
		if claim.PodOwnerReference == nil {
			continue
		}

		claimReleasing, err := isClaimReleasing(claim, podInfos)
		if err != nil {
			log.InfraLogger.Errorf("Error checking if claim is releasing, %v", err)
			continue
		}

		if claimReleasing {
			releasing.Add(*claim.Size)
		}
	}

	return *releasing
}

func isClaimReleasing(
	claim *storageclaim_info.StorageClaimInfo, podInfos map[common_info.PodID]*pod_info.PodInfo) (bool, error) {
	podKey := common_info.PodID(fmt.Sprintf("%s/%s", claim.PodOwnerReference.PodNamespace,
		claim.PodOwnerReference.PodName))
	pod, found := podInfos[podKey]
	if !found {
		return false, fmt.Errorf("failed to find pod %s/%s for PVC %s/%s", claim.PodOwnerReference.PodNamespace,
			claim.PodOwnerReference.PodName, claim.Namespace, claim.Name)
	}

	return !pod_status.IsAliveStatus(pod.Status), nil
}
