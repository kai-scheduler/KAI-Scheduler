// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package storagecapacity_info

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
)

type PVCFitError struct {
	totalRequest        *resource.Quantity
	allocatable         *resource.Quantity
	storageCapacityName string
}

func newPVCFitError(totalRequest, allocatable *resource.Quantity, storageCapacityName string) *PVCFitError {
	return &PVCFitError{
		totalRequest:        totalRequest,
		allocatable:         allocatable,
		storageCapacityName: storageCapacityName,
	}
}

func (p *PVCFitError) Error() string {
	return fmt.Sprintf("total request for pvcs %s exceeds allocatable storage %s on storage capacity %s",
		p.totalRequest.String(), p.allocatable.String(), p.storageCapacityName)
}

type MissingStorageClassError struct {
	storageClassName common_info.StorageClassID
}

func NewMissingStorageClassError(storageClassName common_info.StorageClassID) *MissingStorageClassError {
	return &MissingStorageClassError{storageClassName: storageClassName}
}

func (sce *MissingStorageClassError) Error() string {
	return fmt.Sprintf("no allocatable storage from storageclass %s found", sce.storageClassName)
}
