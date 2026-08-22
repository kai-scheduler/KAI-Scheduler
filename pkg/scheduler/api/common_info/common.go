// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import "k8s.io/apimachinery/pkg/types"

type PodID types.UID

type PodGroupID types.UID

func NewPodGroupID(namespace, name string) PodGroupID {
	return PodGroupID(NewObjectKey(namespace, name))
}

type QueueID types.UID

type StorageClassID types.UID

type StorageCapacityID types.UID

type CSIDriverID types.UID

type ConfigMapID types.UID

type SchedulingConstraintsSignature string
