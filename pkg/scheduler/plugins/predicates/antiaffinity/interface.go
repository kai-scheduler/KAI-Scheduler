// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package antiaffinity

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
)

// BindReadiness checks conflicts with pods that have not finished releasing.
type BindReadiness interface {
	Prepare(*pod_info.PodInfo, []*node_info.NodeInfo) error
	IsReadyForBinding(*pod_info.PodInfo, *node_info.NodeInfo) (bool, error)
}
