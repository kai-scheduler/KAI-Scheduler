// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestReplayPreservesImplicitPodGroupPriority(t *testing.T) {
	registerReplayPlugins(t)
	_, params := replayConfig()
	input := replayObjects()
	require.Empty(t, input.PodGroups[0].Spec.PriorityClassName)
	const clusterPriority int32 = 7777
	input.PriorityClasses = []*schedulingv1.PriorityClass{{
		ObjectMeta:    metav1.ObjectMeta{Name: "cluster-default"},
		GlobalDefault: true,
		Value:         clusterPriority,
	}}

	priorityOfOnlyGroup := func(session *framework.Session) int32 {
		t.Helper()
		require.Len(t, session.ClusterInfo.PodGroupInfos, 1)
		for _, group := range session.ClusterInfo.PodGroupInfos {
			return group.Priority
		}
		panic("unreachable")
	}

	original := openAt(t, params, input, nil, time.Time{})
	originalPriority := priorityOfOnlyGroup(original)
	require.Equal(t, clusterPriority, originalPriority)
	var record *cycleRecord
	rec := newPlugin(nil, func(cycle *cycleRecord) bool {
		record = cycle
		return true
	})
	rec.OnSessionOpen(original)
	rec.OnSessionClose(original)
	framework.CloseSession(original)

	require.NotNil(t, record)
	require.NotNil(t, record.RawObjects)
	replayed := openAt(t, record.SchedulerParams, record.RawObjects, record.Usage, record.OpenedAt)
	defer framework.CloseSession(replayed)
	replayedPriority := priorityOfOnlyGroup(replayed)
	t.Logf("original effective priority=%d, recorded PriorityClasses=%d, replay effective priority=%d",
		originalPriority, len(record.RawObjects.PriorityClasses), replayedPriority)
	require.Equal(t, originalPriority, replayedPriority)
}
