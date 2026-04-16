// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package data_lister

import (
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	scheduling "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

type DataLister interface {
	ListPods() ([]*v1.Pod, error)
	ListPodGroups() ([]*schedulingv2alpha2.PodGroup, error)
	ListNodes() ([]*v1.Node, error)
	ListQueues() ([]*schedulingv2.Queue, error)
	ListPodDisruptionBudgets() ([]*policyv1.PodDisruptionBudget, error)
	ListPriorityClasses() ([]*scheduling.PriorityClass, error)
	GetPriorityClassByName(name string) (*scheduling.PriorityClass, error)
	ListPodByIndex(index, value string) ([]interface{}, error)
	ListPersistentVolumeClaims() ([]*v1.PersistentVolumeClaim, error)
	ListCSIStorageCapacities() ([]*storage.CSIStorageCapacity, error)
	ListStorageClasses() ([]*storage.StorageClass, error)
	ListCSIDrivers() ([]*storage.CSIDriver, error)
	ListBindRequests() ([]*schedulingv1alpha2.BindRequest, error)
	ListConfigMaps() ([]*v1.ConfigMap, error)
}
