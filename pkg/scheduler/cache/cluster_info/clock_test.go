// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClusterInfoClockUsesTheRecordedCycleTime(t *testing.T) {
	info := &ClusterInfo{}
	require.WithinDuration(t, time.Now(), info.clock(), time.Second)

	opened := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	info.SetNow(func() time.Time { return opened })
	require.True(t, info.clock().Equal(opened))
}
