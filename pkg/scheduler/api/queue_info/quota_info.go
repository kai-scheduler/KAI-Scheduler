// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package queue_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	v1 "k8s.io/api/core/v1"
)

type QueueQuota struct {
	GPU    ResourceQuota `json:"gpu,omitempty"`
	CPU    ResourceQuota `json:"cpu,omitempty"`
	Memory ResourceQuota `json:"memory,omitempty"`
}

type ResourceQuota struct {
	// +optional
	Quota float64 `json:"deserved"`
	// +optional
	OverQuotaWeight float64 `json:"overQuotaWeight"`
	// +optional
	Limit float64 `json:"limit"`
}

type QueueUsage map[v1.ResourceName]float64

type ClusterUsage struct {
	Queues map[common_info.QueueID]QueueUsage `json:"queues"`
}

func NewClusterUsage() *ClusterUsage {
	return &ClusterUsage{
		Queues: make(map[common_info.QueueID]QueueUsage),
	}
}
