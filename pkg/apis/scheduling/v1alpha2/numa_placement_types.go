// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha2

import v1 "k8s.io/api/core/v1"

// NUMAZonePlacement is a pod's durable per-zone placement record: the NRT zone id and the
// per-resource amounts placed there. Serialized to kai.scheduler/numa-placement-observed and
// kai.scheduler/numa-placement-predicted.
type NUMAZonePlacement struct {
	Zone   string          `json:"zone"`
	Amount v1.ResourceList `json:"amount"`
}
