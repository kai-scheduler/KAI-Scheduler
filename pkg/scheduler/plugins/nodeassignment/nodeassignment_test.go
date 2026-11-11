// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeassignment

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

const (
	native     = 2 * scores.NodeAssignment
	unassigned = scores.NodeAssignment
	foreign    = 0
)

func TestNodeOrder(t *testing.T) {
	tests := []struct {
		name      string
		args      framework.PluginArguments
		nodeLabel map[string]string
		jobQueue  common_info.QueueID
		want      float64
	}{
		{
			name:     "unassigned node",
			jobQueue: "team-a",
			want:     unassigned,
		},
		{
			name:      "node owned by the pod's queue",
			nodeLabel: map[string]string{DefaultLabelKey: "team-a"},
			jobQueue:  "team-a",
			want:      native,
		},
		{
			name:      "node owned by another queue",
			nodeLabel: map[string]string{DefaultLabelKey: "team-b"},
			jobQueue:  "team-a",
			want:      foreign,
		},
		{
			name:      "node owned by a queue that does not exist",
			nodeLabel: map[string]string{DefaultLabelKey: "missing"},
			jobQueue:  "team-a",
			want:      foreign,
		},
		{
			name:      "node owned by a non-leaf queue",
			nodeLabel: map[string]string{DefaultLabelKey: "department"},
			jobQueue:  "team-a",
			want:      foreign,
		},
		{
			name:      "node assigned with an empty value",
			nodeLabel: map[string]string{DefaultLabelKey: ""},
			jobQueue:  "team-a",
			want:      foreign,
		},
		{
			name:      "custom label key",
			args:      framework.PluginArguments{labelKeyArg: "example.com/owner"},
			nodeLabel: map[string]string{"example.com/owner": "team-a"},
			jobQueue:  "team-a",
			want:      native,
		},
		{
			name:      "default label key is ignored when a custom key is set",
			args:      framework.PluginArguments{labelKeyArg: "example.com/owner"},
			nodeLabel: map[string]string{DefaultLabelKey: "team-b"},
			jobQueue:  "team-a",
			want:      unassigned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNode("node-1", tt.nodeLabel)
			task := &pod_info.PodInfo{Name: "pod-1", Job: "job-1"}
			ssn := newSession(
				[]*node_info.NodeInfo{node},
				&podgroup_info.PodGroupInfo{UID: "job-1", Queue: tt.jobQueue},
			)

			plugin := New(tt.args)
			plugin.OnSessionOpen(ssn)
			require.Len(t, ssn.NodeOrderFns, 1)

			score, err := ssn.NodeOrderFns[0](task, node)
			require.NoError(t, err)
			require.Equal(t, tt.want, score)
		})
	}
}

func TestNodeOrderDoesNotOverrideTopology(t *testing.T) {
	lowerTiers := scores.MaxHighDensity + scores.ResourceType + scores.Numa + scores.Availability + scores.GpuSharing
	require.Greater(t, float64(unassigned-foreign), float64(lowerTiers))
	require.Greater(t, float64(native-unassigned), float64(lowerTiers))
	require.Less(t, float64(native), float64(scores.Topology))
}

func TestOnSessionCloseClearsState(t *testing.T) {
	ssn := newSession([]*node_info.NodeInfo{newNode("node-1", map[string]string{DefaultLabelKey: "team-a"})})
	plugin := New(nil).(*nodeAssignmentPlugin)

	plugin.OnSessionOpen(ssn)
	require.Len(t, plugin.owners, 1)

	plugin.OnSessionClose(ssn)
	require.Nil(t, plugin.owners)
	require.Nil(t, plugin.ssn)
}

func newSession(nodes []*node_info.NodeInfo, jobs ...*podgroup_info.PodGroupInfo) *framework.Session {
	department := &queue_info.QueueInfo{UID: "department", ChildQueues: []common_info.QueueID{"team-a", "team-b"}}
	clusterInfo := &api.ClusterInfo{
		Nodes:         map[string]*node_info.NodeInfo{},
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{},
		Queues: map[common_info.QueueID]*queue_info.QueueInfo{
			"department": department,
			"team-a":     {UID: "team-a", ParentQueue: "department"},
			"team-b":     {UID: "team-b", ParentQueue: "department"},
		},
	}
	for _, node := range nodes {
		clusterInfo.Nodes[node.Name] = node
	}
	for _, job := range jobs {
		clusterInfo.PodGroupInfos[job.UID] = job
	}
	return &framework.Session{ClusterInfo: clusterInfo}
}

func newNode(name string, labels map[string]string) *node_info.NodeInfo {
	return &node_info.NodeInfo{
		Name: name,
		Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}},
	}
}
