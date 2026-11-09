// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package preempt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

// A failed resize-only job has no demand and would make every later job in its queue look harder.
func TestPreemptForPendingTasksSkipsJobWithoutPendingTasks(t *testing.T) {
	ssn, _, running, _ := newPreemptVictimQueueTestSession()
	require.Zero(t, running.GetNumPendingTasks())

	smallestFailedJobsByQueue := map[common_info.QueueID]*common.MinimalJobRepresentatives{}
	require.False(t, preemptForPendingTasks(ssn, running, nil, smallestFailedJobsByQueue))
	require.Empty(t, smallestFailedJobsByQueue)
}
