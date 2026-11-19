// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	wire "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/replay"
)

func TestReplayPreservesEqualTaintPlacements(t *testing.T) {
	registerReplayPlugins(t)
	input := &cycleRecord{}
	input.Config, input.SchedulerParams = replayConfig()
	input.RawObjects = &snapshot.RawKubernetesObjects{Queues: replayObjects().Queues}
	h := wire.NewHasher([]byte("synthetic-test-key"))
	names := map[string]string{}
	taintValues := map[string]string{}

	for _, color := range []string{"blue", "red"} {
		n := cpuNode("node-"+color, "4")
		n.Labels = map[string]string{v1.LabelHostname: n.Name}
		n.Spec.Taints = []v1.Taint{{Key: "team", Value: color, Effect: v1.TaintEffectNoSchedule}}
		g := podGroup("test", "group-"+color, "team-a", "")
		p := workloadPod("test", "pod-"+color, g.Name, "", "100m", v1.PodPending)
		p.Spec.NodeSelector = map[string]string{v1.LabelHostname: n.Name}
		p.Spec.Tolerations = []v1.Toleration{{
			Key: "team", Operator: v1.TolerationOpEqual, Value: "blue", Effect: v1.TaintEffectNoSchedule,
		}}
		input.RawObjects.Nodes = append(input.RawObjects.Nodes, n)
		input.RawObjects.PodGroups = append(input.RawObjects.PodGroups, g)
		input.RawObjects.Pods = append(input.RawObjects.Pods, p)
		taintValues[h.Hash(n.Name)] = color
		for _, name := range []string{n.Name, p.Name} {
			names[name], names[h.Hash(name)] = name, name
		}
	}

	run := func(label string, in *cycleRecord) (map[string]string, *cycleRecord) {
		t.Helper()
		cache, stop, err := replay.NewReplayCache(in.SchedulerParams, in.RawObjects, in.Usage, in.OpenedAt)
		require.NoError(t, err)
		defer stop()
		s, err := framework.OpenSession(cache, in.Config, in.SchedulerParams, "taint-test", http.NewServeMux())
		require.NoError(t, err)
		defer framework.CloseSession(s)

		var record *cycleRecord
		rec := newPlugin(nil, func(r *cycleRecord) bool { record = r; return true })
		rec.OnSessionOpen(s)
		s.SetCurrentAction(framework.Allocate)
		allocate.New().Execute(s)
		rec.OnSessionClose(s)
		require.NotNil(t, record)

		out := map[string]string{}
		for _, g := range s.ClusterInfo.PodGroupInfos {
			for _, p := range g.GetPendingTasks() {
				out[names[p.Name]] = "pending"
			}
		}
		for _, event := range record.Decisions {
			for _, op := range event.Ops {
				require.Equal(t, framework.CommitOpAllocate, op.Kind)
				require.False(t, op.Failed)
				require.False(t, op.NotAttempted)
				require.NotContains(t, out, names[op.Name])
				out[names[op.Name]] = names[op.Node]
			}
		}
		t.Logf("%s: pod-blue=%s pod-red=%s", label, out["pod-blue"], out["pod-red"])
		return out, record
	}

	before, record := run("original", input)
	require.Equal(t, map[string]string{"pod-blue": "node-blue", "pod-red": "pending"}, before)
	plain, _ := run("unredacted replay", record)
	require.Equal(t, before, plain)

	env := &wire.Envelope{SchemaVersion: wire.SchemaVersion, Kind: wire.KindCycle, Record: mustJSON(t, record)}
	require.NoError(t, wire.RedactEnvelope(env, h))
	var redacted, restored cycleRecord
	require.NoError(t, json.Unmarshal(env.Record, &redacted))
	require.NoError(t, json.Unmarshal(env.Record, &restored))
	after, _ := run("redacted replay", &redacted)

	for _, n := range restored.RawObjects.Nodes {
		n.Spec.Taints[0].Value = taintValues[n.Name]
	}
	for _, p := range restored.RawObjects.Pods {
		p.Spec.Tolerations[0].Value = "blue"
	}
	control, _ := run("redacted replay with values restored", &restored)
	require.Equal(t, before, control)
	require.Equal(t, before, after, "redaction must preserve placement")
}
