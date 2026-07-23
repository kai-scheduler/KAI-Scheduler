// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestNewCachedVictimsQueueGenerator(t *testing.T) {
	job := podGroupForJobOrderTest("victim", "victim", 1)
	job.Queue = testQueue
	ssn := &framework.Session{
		ClusterInfo: &api.ClusterInfo{
			Queues: map[common_info.QueueID]*queue_info.QueueInfo{
				testQueue: {UID: testQueue},
			},
		},
	}

	discoveryCalls := 0
	generateVictimsQueue := NewCachedVictimsQueueGenerator(
		ssn,
		func() map[common_info.PodGroupID]*podgroup_info.PodGroupInfo {
			discoveryCalls++
			return map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{job.UID: job}
		},
		JobsOrderInitOptions{},
	)

	firstQueue := generateVictimsQueue()
	secondQueue := generateVictimsQueue()

	require.Equal(t, 1, discoveryCalls)
	require.NotSame(t, firstQueue, secondQueue)
	require.Same(t, job, firstQueue.PopNextJob())
	require.Same(t, job, secondQueue.PopNextJob())
}
