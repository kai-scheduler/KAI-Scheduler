// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	wire "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions/allocate"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/topology"
	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestReplayRedactedRackZoneColocation(t *testing.T) {
	registerReplayPlugins(t)
	framework.RegisterPluginBuilder("topology", topology.New)
	config, params := replayConfig()
	config.Tiers[0].Plugins = append(config.Tiers[0].Plugins, conf.PluginOption{Name: "topology"})

	objects := &snapshot.RawKubernetesObjects{Queues: replayObjects().Queues}
	objects.Topologies = []*kaiv1alpha1.Topology{{
		ObjectMeta: metav1.ObjectMeta{Name: "dc"},
		Spec: kaiv1alpha1.TopologySpec{Levels: []kaiv1alpha1.TopologyLevel{
			{NodeLabel: "topology.kubernetes.io/zone"},
			{NodeLabel: "example.com/rack", Alias: "rack"},
		}},
	}}
	group := podGroup("team", "colocated", "team-a", "")
	group.Spec.MinMember = ptr.To(int32(2))
	group.Spec.TopologyConstraint = enginev2alpha2.TopologyConstraint{
		Topology:              "dc",
		RequiredTopologyLevel: "rack",
	}
	objects.PodGroups = []*enginev2alpha2.PodGroup{group}
	for _, name := range []string{"node-a1", "node-a2"} {
		node := labeledNode(name, "1", map[string]string{
			"topology.kubernetes.io/zone": "zone-a",
			"example.com/rack":            "rack-a",
		})
		objects.Nodes = append(objects.Nodes, node)
	}
	objects.Nodes = append(objects.Nodes, labeledNode("node-open", "4", map[string]string{
		"topology.kubernetes.io/zone": "zone-b",
	}))
	objects.Pods = []*v1.Pod{
		workloadPod("team", "pod-0", group.Name, "", "600m", v1.PodPending),
		workloadPod("team", "pod-1", group.Name, "", "600m", v1.PodPending),
	}

	want := map[string]string{"pod-0": "rack-a", "pod-1": "rack-a"}
	got := redactedPlacements(t, config, params, objects, func(node string) string {
		switch node {
		case "node-a1", "node-a2":
			return "rack-a"
		case "pending":
			return "pending"
		default:
			return node
		}
	})
	require.Equal(t, want, got)
}

func TestReplayRedactedRequiredNodeAffinity(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	objects := &snapshot.RawKubernetesObjects{Queues: replayObjects().Queues}
	objects.Nodes = []*v1.Node{
		labeledNode("node-blue", "2", map[string]string{"example.com/pool": "blue", "example.com/generation": "1"}),
		labeledNode("node-new", "2", map[string]string{"example.com/pool": "red", "example.com/generation": "5"}),
	}
	poolGroup := podGroup("team", "pool", "team-a", "")
	genGroup := podGroup("team", "generation", "team-a", "")
	objects.PodGroups = []*enginev2alpha2.PodGroup{poolGroup, genGroup}
	poolPod := workloadPod("team", "pool-pod", poolGroup.Name, "", "100m", v1.PodPending)
	poolPod.Spec.Affinity = nodeAffinity(v1.NodeSelectorRequirement{
		Key: "example.com/pool", Operator: v1.NodeSelectorOpIn, Values: []string{"blue"},
	})
	genPod := workloadPod("team", "gen-pod", genGroup.Name, "", "100m", v1.PodPending)
	genPod.Spec.Affinity = nodeAffinity(v1.NodeSelectorRequirement{
		Key: "example.com/generation", Operator: v1.NodeSelectorOpGt, Values: []string{"2"},
	})
	objects.Pods = []*v1.Pod{poolPod, genPod}

	got := redactedPlacements(t, config, params, objects, func(node string) string { return node })
	require.Equal(t, map[string]string{"pool-pod": "node-blue", "gen-pod": "node-new"}, got)
}

func TestReplayRedactedPodAffinitySameAndDifferentNode(t *testing.T) {
	registerReplayPlugins(t)
	config, params := replayConfig()
	objects := &snapshot.RawKubernetesObjects{Queues: replayObjects().Queues}
	objects.Nodes = []*v1.Node{
		labeledNode("node-a", "4", map[string]string{"example.com/segment": "alpha"}),
		labeledNode("node-b", "4", map[string]string{"example.com/segment": "beta"}),
	}
	anchor := podGroup("ns-a", "anchor", "team-a", "")
	apart := podGroup("ns-a", "apart-anchor", "team-a", "")
	together := podGroup("ns-b", "together", "team-a", "")
	split := podGroup("ns-b", "split", "team-a", "")
	objects.PodGroups = []*enginev2alpha2.PodGroup{anchor, apart, together, split}

	runningTogether := workloadPod("ns-a", "running-together", anchor.Name, "node-a", "100m", v1.PodRunning)
	runningTogether.Labels = map[string]string{"app": "together"}
	runningApart := workloadPod("ns-a", "running-apart", apart.Name, "node-a", "100m", v1.PodRunning)
	runningApart.Labels = map[string]string{"app": "apart"}
	same := workloadPod("ns-b", "same-pod", together.Name, "", "100m", v1.PodPending)
	same.Spec.Affinity = &v1.Affinity{PodAffinity: &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "together"}},
			TopologyKey:   "example.com/segment",
			Namespaces:    []string{"ns-a"},
		}},
	}}
	other := workloadPod("ns-b", "other-pod", split.Name, "", "100m", v1.PodPending)
	other.Spec.Affinity = &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "apart"}},
			TopologyKey:   "example.com/segment",
			Namespaces:    []string{"ns-a"},
		}},
	}}
	objects.Pods = []*v1.Pod{runningTogether, runningApart, same, other}

	got := redactedPlacements(t, config, params, objects, func(node string) string { return node })
	require.Equal(t, map[string]string{"same-pod": "node-a", "other-pod": "node-b"}, got)
}

func labeledNode(name, cpu string, labels map[string]string) *v1.Node {
	node := cpuNode(name, cpu)
	node.Labels = labels
	return node
}

func nodeAffinity(requirement v1.NodeSelectorRequirement) *v1.Affinity {
	return &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{requirement}}},
		},
	}}
}

// redactedPlacements runs the original cycle, an unredacted replay, and a redacted replay.
// bucket maps a node name to the placement class the test compares. Pending stays "pending".
func redactedPlacements(
	t *testing.T, config *conf.SchedulerConfiguration, params *conf.SchedulerParams,
	objects *snapshot.RawKubernetesObjects, bucket func(string) string,
) map[string]string {
	t.Helper()
	input := &cycleRecord{Snapshot: snapshot.Snapshot{RawObjects: objects}}
	input.Config = config
	input.SchedulerParams = params
	names := map[string]string{}
	interesting := map[string]struct{}{}
	for _, pod := range objects.Pods {
		names[pod.Name] = pod.Name
		if pod.Spec.NodeName == "" {
			interesting[pod.Name] = struct{}{}
		}
	}
	for _, node := range objects.Nodes {
		names[node.Name] = node.Name
	}
	h := wire.NewHasher([]byte("synthetic-test-key"))
	for name := range names {
		names[h.Hash(name)] = name
	}

	run := func(in *cycleRecord) (map[string]string, *cycleRecord) {
		t.Helper()
		cache := startReplayCache(t, in.SchedulerParams, in.RawObjects, in.Usage, in.OpenedAt)
		session, err := framework.OpenSession(cache, in.Config, in.SchedulerParams, "placement", http.NewServeMux())
		require.NoError(t, err)
		defer framework.CloseSession(session)
		var record *cycleRecord
		plugin := newPlugin(nil, func(cycle *cycleRecord) bool { record = cycle; return true })
		plugin.OnSessionOpen(session)
		session.SetCurrentAction(framework.Allocate)
		allocate.New().Execute(session)
		plugin.OnSessionClose(session)
		require.NotNil(t, record)
		out := map[string]string{}
		for _, group := range session.ClusterInfo.PodGroupInfos {
			for _, pod := range group.GetPendingTasks() {
				original := names[pod.Name]
				if _, ok := interesting[original]; !ok {
					continue
				}
				out[original] = bucket("pending")
			}
		}
		for _, event := range record.Decisions {
			for _, op := range event.Ops {
				if op.NotAttempted || op.Failed || op.Kind != framework.CommitOpAllocate {
					continue
				}
				original := names[op.Name]
				if _, ok := interesting[original]; !ok {
					continue
				}
				node := names[op.Node]
				if node == "" {
					node = op.Node
				}
				out[original] = bucket(node)
			}
		}
		return out, record
	}

	before, recorded := run(input)
	plain, _ := run(recorded)
	require.Equal(t, before, plain)
	env := &wire.Envelope{SchemaVersion: wire.SchemaVersion, Kind: wire.KindCycle, Record: mustJSON(t, recorded)}
	require.NoError(t, wire.RedactEnvelope(env, h))
	var redacted cycleRecord
	require.NoError(t, json.Unmarshal(env.Record, &redacted))
	after, _ := run(&redacted)
	require.Equal(t, before, after)
	return before
}
