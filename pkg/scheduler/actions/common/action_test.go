// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

func TestTaskAllocationModeForPipelining(t *testing.T) {
	preemptor := podgroup_info.NewPodGroupInfo("preemptor")
	victim := podgroup_info.NewPodGroupInfo("victim")

	require.Equal(t, podgroup_info.SimulatedTaskAllocation,
		taskAllocationModeForPipelining(preemptor, preemptor, podgroup_info.SimulatedTaskAllocation))
	require.Equal(t, podgroup_info.PartialTaskAllocation,
		taskAllocationModeForPipelining(preemptor, preemptor, podgroup_info.PartialTaskAllocation))
	require.Equal(t, podgroup_info.VictimReallocation,
		taskAllocationModeForPipelining(victim, preemptor, podgroup_info.PartialTaskAllocation))
}
