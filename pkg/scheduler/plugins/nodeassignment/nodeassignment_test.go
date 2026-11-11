// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodeassignment

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
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
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
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

func TestPredicate(t *testing.T) {
	tests := []struct {
		name           string
		nodeLabel      map[string]string
		preemptibility v2alpha2.Preemptibility
		wantDetail     string
	}{
		{
			name:           "non-preemptible pod on an unassigned node",
			preemptibility: v2alpha2.NonPreemptible,
		},
		{
			name:           "non-preemptible pod on a node owned by its queue",
			nodeLabel:      map[string]string{DefaultLabelKey: "team-a"},
			preemptibility: v2alpha2.NonPreemptible,
		},
		{
			name:           "preemptible pod on a node owned by another queue",
			nodeLabel:      map[string]string{DefaultLabelKey: "team-b"},
			preemptibility: v2alpha2.Preemptible,
		},
		{
			name:           "non-preemptible pod on a node owned by another queue",
			nodeLabel:      map[string]string{DefaultLabelKey: "team-b"},
			preemptibility: v2alpha2.NonPreemptible,
			wantDetail:     "node is assigned to queue team-b; non-preemptible pods of other queues are not allowed",
		},
		{
			name:           "semi-preemptible pod on a node owned by another queue",
			nodeLabel:      map[string]string{DefaultLabelKey: "team-b"},
			preemptibility: v2alpha2.SemiPreemptible,
			wantDetail:     "node is assigned to queue team-b; non-preemptible pods of other queues are not allowed",
		},
		{
			name:           "non-preemptible pod on a node owned by a queue that does not exist",
			nodeLabel:      map[string]string{DefaultLabelKey: "missing"},
			preemptibility: v2alpha2.NonPreemptible,
			wantDetail:     "node is assigned to queue missing; non-preemptible pods of other queues are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNode("node-1", tt.nodeLabel)
			task := &pod_info.PodInfo{Name: "pod-1", Namespace: "ns", Job: "job-1"}
			job := &podgroup_info.PodGroupInfo{UID: "job-1", Queue: "team-a", Preemptibility: tt.preemptibility}
			ssn := newSession([]*node_info.NodeInfo{node}, job)

			New(nil).OnSessionOpen(ssn)
			require.Len(t, ssn.PredicateFns, 1)

			err := ssn.PredicateFns[0](task, job, node)
			if tt.wantDetail == "" {
				require.NoError(t, err)
				return
			}
			var fitErr *common_info.TasksFitError
			require.ErrorAs(t, err, &fitErr)
			require.Equal(t, "node-1", fitErr.NodeName)
			require.Equal(t, []string{foreignNonPreemptibleReason}, fitErr.Reasons)
			require.Equal(t, []string{tt.wantDetail}, fitErr.DetailedReasons)
		})
	}
}

func TestScoreTier(t *testing.T) {
	var lowerTiers, nextTier int64
	for _, tier := range scoreTiers(t) {
		switch {
		case tier < scores.NodeAssignment:
			lowerTiers += tier
		case tier > scores.NodeAssignment && (nextTier == 0 || tier < nextTier):
			nextTier = tier
		}
	}

	require.Greater(t, float64(unassigned-foreign), float64(lowerTiers), "lower tiers can override node assignment")
	require.Greater(t, float64(native-unassigned), float64(lowerTiers), "lower tiers can override node assignment")
	require.Less(t, float64(native), float64(nextTier), "node assignment can override the next tier")
}

// scoreTiers reads every constant in the scores package, so new tiers are covered without editing this test.
func scoreTiers(t *testing.T) []int64 {
	file, err := parser.ParseFile(token.NewFileSet(), "../scores/scores.go", nil, 0)
	require.NoError(t, err)

	var tiers []int64
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, value := range spec.(*ast.ValueSpec).Values {
				lit, ok := value.(*ast.BasicLit)
				require.True(t, ok, "score tiers must be integer literals")
				tier, err := strconv.ParseInt(lit.Value, 10, 64)
				require.NoError(t, err)
				tiers = append(tiers, tier)
			}
		}
	}
	require.Contains(t, tiers, int64(scores.NodeAssignment))
	return tiers
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
