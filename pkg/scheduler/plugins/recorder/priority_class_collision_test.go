// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestReplayPreservesDistinctPrioritiesWithNameCollision(t *testing.T) {
	registerReplayPlugins(t)
	_, params := replayConfig()

	priorities := func(session *framework.Session) map[string]int32 {
		result := map[string]int32{}
		for _, group := range session.ClusterInfo.PodGroupInfos {
			result[group.Name] = group.Priority
		}
		return result
	}

	for _, className := range []string{"ordinary-explicit", "kai-effective-7777"} {
		t.Run(className, func(t *testing.T) {
			input := replayObjects()
			input.PriorityClasses = []*schedulingv1.PriorityClass{
				{ObjectMeta: metav1.ObjectMeta{Name: "cluster-default"}, GlobalDefault: true, Value: 7777},
				{ObjectMeta: metav1.ObjectMeta{Name: className}, Value: 123},
			}
			extra := podGroup("team-a", "explicit", "team-a", className)
			input.PodGroups = append(input.PodGroups, extra)
			input.Pods = append(input.Pods, workloadPod("team-a", "explicit-0", extra.Name, "", "100m", v1.PodPending))

			original := openAt(t, params, input, nil, time.Time{})
			before := priorities(original)
			require.Equal(t, map[string]int32{"job-a": 7777, "explicit": 123}, before)
			var record *cycleRecord
			plugin := newPlugin(nil, func(cycle *cycleRecord) bool {
				record = cycle
				return true
			})
			plugin.OnSessionOpen(original)
			plugin.OnSessionClose(original)
			framework.CloseSession(original)
			require.NotNil(t, record)
			require.NotNil(t, record.RawObjects)

			replayed := openAt(t, record.SchedulerParams, record.RawObjects, record.Usage, record.OpenedAt)
			defer framework.CloseSession(replayed)
			after := priorities(replayed)
			t.Logf("original inherited=%d explicit=%d; recorded classes=%d; replay inherited=%d explicit=%d",
				before["job-a"], before["explicit"], len(record.RawObjects.PriorityClasses), after["job-a"], after["explicit"])
			require.Equal(t, before, after)
		})
	}
}
