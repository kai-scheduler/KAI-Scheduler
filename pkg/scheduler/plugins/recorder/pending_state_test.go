// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestPendingGroupsRecordAttemptAndSkip(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	objects := &snapshot.RawKubernetesObjects{
		Queues: replayObjects().Queues,
		Nodes:  []*v1.Node{cpuNode("node-a", "1")},
	}
	tried := podGroup("team", "tried", "team-a", "")
	tried.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{{
		Reason: "StaleReasonFromStatus",
		Reasons: enginev2alpha2.UnschedulableExplanations{{
			Reason: "StaleReasonFromStatus",
		}},
	}}
	skipped := podGroup("team", "skipped", "team-a", "")
	skipped.Spec.MinMember = ptr.To(int32(2))
	objects.PodGroups = []*enginev2alpha2.PodGroup{tried, skipped}
	objects.Pods = []*v1.Pod{
		workloadPod("team", "big", tried.Name, "", "4", v1.PodPending),
		workloadPod("team", "alone", skipped.Name, "", "100m", v1.PodPending),
	}

	recorded := runAllocateSession(t, newPlugin(nil, func(*cycleRecord) bool { return true }), config, params, objects)
	require.NotNil(t, recorded)
	byName := map[string]pendingGroup{}
	for _, group := range recorded.Pending {
		byName[group.Name] = group
	}
	require.Contains(t, byName, "tried")
	require.True(t, byName["tried"].Attempted)
	require.NotEmpty(t, byName["tried"].ReasonCodes)
	require.NotContains(t, byName["tried"].ReasonCodes, "StaleReasonFromStatus")
	require.Contains(t, byName, "skipped")
	require.False(t, byName["skipped"].Attempted)
	require.Empty(t, byName["skipped"].ReasonCodes)
}

func TestNodeAddCyclesAreRecordedAndReplayable(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	var records []*cycleRecord
	plugin := newPlugin(nil, func(cycle *cycleRecord) bool {
		copied := *cycle
		records = append(records, &copied)
		return true
	})

	without := replayObjects()
	without.Nodes = nil
	runAllocateSession(t, plugin, config, params, without)
	runAllocateSession(t, plugin, config, params, replayObjects())

	require.Len(t, records, 2)
	require.Empty(t, records[0].Decisions)
	require.NotEmpty(t, records[0].Pending)
	require.True(t, records[0].Pending[0].Attempted)
	require.NotEmpty(t, records[0].StateID)
	require.NotNil(t, records[0].RawObjects)
	require.False(t, records[0].StateMissing)

	require.NotEmpty(t, records[1].Decisions)
	require.NotEqual(t, records[0].StateID, records[1].StateID)
	require.NotNil(t, records[1].RawObjects)
	require.False(t, records[1].StateMissing)

	first := runAllocateSession(t, newPlugin(nil, func(*cycleRecord) bool { return true }), config, params, records[0].RawObjects)
	require.Empty(t, first.Decisions)
	second := runAllocateSession(t, newPlugin(nil, func(*cycleRecord) bool { return true }), config, params, records[1].RawObjects)
	require.NotEmpty(t, second.Decisions)
	require.Equal(t, decisionLines(records[1]), decisionLines(second))
}

func TestUnchangedStateIsReusedUntilInterval(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	var records []*cycleRecord
	plugin := newPlugin(nil, func(cycle *cycleRecord) bool {
		copied := *cycle
		records = append(records, &copied)
		return true
	})
	closeWithoutAction(t, plugin, config, params, idleObjects())
	closeWithoutAction(t, plugin, config, params, idleObjects())
	require.Len(t, records, 2)
	require.NotEmpty(t, records[0].StateID)
	require.Equal(t, records[0].StateID, records[1].StateID)
	require.NotNil(t, records[0].RawObjects)
	require.Nil(t, records[1].RawObjects)
	require.Nil(t, records[1].Config)
	require.False(t, records[1].StateMissing)

	require.NoError(t, ResolveStates(records))
	require.NotNil(t, records[1].RawObjects)
	require.Equal(t, records[0].RawObjects, records[1].RawObjects)
}

func TestStateIsSavedAgainWhenIntervalElapses(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	var records []*cycleRecord
	plugin := newPlugin(framework.PluginArguments{"stateInterval": "0s"}, func(cycle *cycleRecord) bool {
		copied := *cycle
		records = append(records, &copied)
		return true
	})
	closeWithoutAction(t, plugin, config, params, idleObjects())
	closeWithoutAction(t, plugin, config, params, idleObjects())
	require.Len(t, records, 2)
	require.NotNil(t, records[0].RawObjects)
	require.NotNil(t, records[1].RawObjects)
	require.Equal(t, records[0].StateID, records[1].StateID)
}

func TestReplayReportsMissingState(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	var records []*cycleRecord
	plugin := newPlugin(nil, func(cycle *cycleRecord) bool {
		copied := *cycle
		records = append(records, &copied)
		return true
	})
	closeWithoutAction(t, plugin, config, params, idleObjects())
	closeWithoutAction(t, plugin, config, params, idleObjects())
	require.Len(t, records, 2)

	markMissingState(records[1], nil)
	require.True(t, records[1].StateMissing)
	err := ResolveStates(records)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing")

	records[1].StateMissing = false
	err = ResolveStates([]*cycleRecord{records[1]})
	require.Error(t, err)
	require.True(t, records[1].StateMissing)
	require.Contains(t, err.Error(), records[1].StateID)
}

func runAllocateSession(
	t *testing.T, plugin *recorderPlugin, config *conf.SchedulerConfiguration, params *conf.SchedulerParams,
	objects *snapshot.RawKubernetesObjects,
) *cycleRecord {
	t.Helper()
	var recorded *cycleRecord
	previous := plugin.enqueue
	plugin.enqueue = func(cycle *cycleRecord) bool {
		recorded = cycle
		if previous != nil {
			return previous(cycle)
		}
		return true
	}
	t.Cleanup(func() { plugin.enqueue = previous })
	cache := startReplayCache(t, params, objects, nil, time.Time{})
	session, err := framework.OpenSession(cache, config, params, "pending-state", http.NewServeMux())
	require.NoError(t, err)
	plugin.OnSessionOpen(session)
	session.SetCurrentAction(framework.Allocate)
	allocate.New().Execute(session)
	plugin.OnSessionClose(session)
	framework.CloseSession(session)
	require.NotNil(t, recorded)
	return recorded
}

func closeWithoutAction(
	t *testing.T, plugin *recorderPlugin, config *conf.SchedulerConfiguration, params *conf.SchedulerParams,
	objects *snapshot.RawKubernetesObjects,
) {
	t.Helper()
	cache := startReplayCache(t, params, objects, nil, time.Time{})
	session, err := framework.OpenSession(cache, config, params, "state-reuse", http.NewServeMux())
	require.NoError(t, err)
	plugin.OnSessionOpen(session)
	plugin.OnSessionClose(session)
	framework.CloseSession(session)
}

func idleObjects() *snapshot.RawKubernetesObjects {
	out := replayObjects()
	out.Nodes = nil
	fixed := metav1.NewTime(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
	for _, queue := range out.Queues {
		queue.UID = types.UID(queue.Name)
		queue.CreationTimestamp = fixed
	}
	for _, group := range out.PodGroups {
		group.UID = types.UID(group.Namespace + "/" + group.Name)
		group.CreationTimestamp = fixed
	}
	for _, pod := range out.Pods {
		pod.CreationTimestamp = fixed
	}
	return out
}

func decisionLines(cycle *cycleRecord) []string {
	var lines []string
	for _, event := range cycle.Decisions {
		for _, op := range event.Ops {
			if op.NotAttempted {
				continue
			}
			lines = append(lines, string(event.Action)+" "+string(op.Kind)+" "+op.Namespace+"/"+op.Name+"@"+op.Node)
		}
	}
	return lines
}
