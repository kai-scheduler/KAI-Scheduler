// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestNoopStatusUpdaterWritesNothing(t *testing.T) {
	updater := NewNoop()
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"}}
	podGroup := &enginev2alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns"}}
	job := &podgroup_info.PodGroupInfo{Name: "pg"}

	require.NotPanics(t, func() {
		updater.SyncPodGroupsWithPendingUpdates([]*enginev2alpha2.PodGroup{podGroup})
		updater.Evicted(pod, podGroup, eviction_info.EvictionMetadata{Action: "reclaim"}, "evicted")
		updater.PreBind(pod)
		updater.Pipelined(pod, "pipelined")
		updater.PatchPodLabels(pod, map[string]interface{}{"kai.scheduler/node-pool": "test"})
		updater.Run(make(chan struct{}))
	})
	require.NoError(t, updater.Bound(pod, "node", errors.New("bind failed"), "pool"))
	require.NoError(t, updater.RecordJobStatusEvent(job, func(
		*podgroup_info.PodGroupInfo, *pod_info.PodInfo,
	) ([]*common_info.TasksFitError, error) {
		t.Fatal("noop must not resolve fit errors")
		return nil, nil
	}))
}
