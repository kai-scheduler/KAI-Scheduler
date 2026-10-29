// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestNumaSolveFailurePreservesPlacement(t *testing.T) {
	task := &pod_info.PodInfo{NUMAPlacement: pod_info.NUMAPlacement{MemoryGroups: []pod_info.MemoryGroupPlacement{{Mask: 3}}}}
	previous := task.NUMAPlacement.Clone()
	session := &framework.Session{NumaPlacementFn: func(*pod_info.PodInfo, *node_info.NodeInfo) (pod_info.NUMAPlacement, error) {
		return pod_info.NUMAPlacement{}, errors.New("insufficient memory group capacity")
	}}
	require.False(t, allocateTaskToNode(session, nil, task, &node_info.NodeInfo{Name: "node"}, false))
	require.True(t, task.NUMAPlacement.Equal(previous))
}
