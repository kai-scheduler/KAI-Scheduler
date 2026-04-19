// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaimable

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

type ReclaimerInfo struct {
	Name              string
	Namespace         string
	Queue             common_info.QueueID
	RequiredResources *resource_info.Resource
	IsPreemptable     bool
}
