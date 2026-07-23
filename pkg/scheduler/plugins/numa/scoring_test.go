// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

// scoringPlugin wires a plugin over nodes and runs the session-open precompute (maxZones, aware
// devices), so nodeScore/wantsNuma see the same state they would in a real session.
func scoringPlugin(nodes map[string]*node_info.NodeInfo) *numaPlugin {
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{Nodes: nodes}}
	pp := &numaPlugin{ignoreList: sets.New[v1.ResourceName](), ssn: ssn}
	pp.initCaches(ssn)
	return pp
}

func TestBestEffortEvaluatorSpan(t *testing.T) {
	// Four NUMA zones, one GPU each: the greedy span is the number of GPUs requested.
	node := numaTopology(node_info.TopologyPolicyBestEffort, node_info.TopologyScopeContainer,
		numaZone("node-0", map[string]string{gpu: "1"}),
		numaZone("node-1", map[string]string{gpu: "1"}),
		numaZone("node-2", map[string]string{gpu: "1"}),
		numaZone("node-3", map[string]string{gpu: "1"}),
	)

	tests := map[string]struct {
		gpus      string
		wantSpan  int
		wantAdmit bool
	}{
		"single zone":         {gpus: "1", wantSpan: 1, wantAdmit: true},
		"spans two zones":     {gpus: "2", wantSpan: 2, wantAdmit: true},
		"full spread":         {gpus: "4", wantSpan: 4, wantAdmit: true},
		"over total capacity": {gpus: "5", wantSpan: 0, wantAdmit: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			placement, ok := evalPlacement(node, noIgnoreList, []v1.ResourceList{req(gpu, test.gpus)})
			assert.Equal(t, test.wantAdmit, ok)
			assert.Len(t, placement, test.wantSpan)
		})
	}
}

func TestNodeScoreTiers(t *testing.T) {
	nodes := map[string]*node_info.NodeInfo{
		"aligned": {Name: "aligned", NumaTopology: numaTopology(node_info.TopologyPolicySingleNUMANode,
			node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "4", "cpu": "16"}),
			numaZone("node-1", map[string]string{gpu: "4", "cpu": "16"}))},
		"restricted-span2": {Name: "restricted-span2", NumaTopology: numaTopology(node_info.TopologyPolicyRestricted,
			node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "1"}),
			numaZone("node-1", map[string]string{gpu: "1"}))},
		"best-effort": {Name: "best-effort", NumaTopology: numaTopology(node_info.TopologyPolicyBestEffort,
			node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "1"}),
			numaZone("node-1", map[string]string{gpu: "1"}))},
		"none-4zone": {Name: "none-4zone", NumaTopology: numaTopology(node_info.TopologyPolicyNone,
			node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "1"}),
			numaZone("node-1", map[string]string{gpu: "1"}),
			numaZone("node-2", map[string]string{gpu: "1"}),
			numaZone("node-3", map[string]string{gpu: "1"}))},
		"no-nrt": {Name: "no-nrt"},
	}

	// maxZones is 4 (the none node), the anchor for the no-NRT worst case.
	tests := map[string]struct {
		task  *pod_info.PodInfo
		node  string
		score float64
	}{
		"aligned single zone":       {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), node: "aligned", score: scores.Numa / 1},
		"restricted forced span 2":  {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 2), node: "restricted-span2", score: scores.Numa / 2},
		"restricted infeasible":     {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 3), node: "restricted-span2", score: 0},
		"best-effort aligned":       {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), node: "best-effort", score: scores.Numa / 1},
		"best-effort full spread":   {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 2), node: "best-effort", score: scores.Numa / 2},
		"none worst-case full span": {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), node: "none-4zone", score: scores.Numa / 4},
		"no-nrt uses maxZones":      {task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), node: "no-nrt", score: scores.Numa / 4},
		"non-numa task is silent":   {task: makeTask(v1.PodQOSBestEffort, pod_info.RequestTypeRegular, 0), node: "aligned", score: 0},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pp := scoringPlugin(nodes)
			score, err := pp.nodeScore(test.task, nodes[test.node])
			assert.NoError(t, err)
			assert.Equal(t, test.score, score)
		})
	}
}

func TestWantsNuma(t *testing.T) {
	pp := scoringPlugin(map[string]*node_info.NodeInfo{
		"gpu-node": {Name: "gpu-node", NumaTopology: singleNUMANodeTopology(node_info.TopologyScopeContainer,
			numaZone("node-0", map[string]string{gpu: "4", "cpu": "16"}))},
	})

	assert.True(t, pp.wantsNuma(makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 0)), "guaranteed is sensitive")
	assert.True(t, pp.wantsNuma(makeTask(v1.PodQOSBurstable, pod_info.RequestTypeRegular, 1)), "burstable requesting a gpu is sensitive")
	assert.False(t, pp.wantsNuma(makeBurstableTask(req("cpu", "2"))), "burstable cpu-only is not sensitive")
	assert.False(t, pp.wantsNuma(makeTask(v1.PodQOSBestEffort, pod_info.RequestTypeRegular, 0)), "best-effort qos without devices is not sensitive")
}
