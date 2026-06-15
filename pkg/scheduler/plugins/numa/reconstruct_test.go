// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func cpuMap(q string) map[v1.ResourceName]resource.Quantity {
	return map[v1.ResourceName]resource.Quantity{"cpu": resource.MustParse(q)}
}

func TestNewReconstructAvailableFlag(t *testing.T) {
	off := New(framework.PluginArguments{}).(*numaPlugin)
	assert.False(t, off.reconstructAvailable, "defaults to trusting NRT Available")

	on := New(framework.PluginArguments{reconstructAvailableArg: "true"}).(*numaPlugin)
	assert.True(t, on.reconstructAvailable)
}

func TestReconstructNodeAvailable(t *testing.T) {
	// Two zones, Allocatable 4 CPU each. Available is deliberately stale (99) to prove reconstruction
	// discards the NRT-reported value and rebuilds from Allocatable.
	topo := &node_info.NumaTopology{
		Policy: node_info.TopologyPolicySingleNUMANode,
		Scope:  node_info.TopologyScopePod,
		Zones: []*node_info.NumaZone{
			{ID: "node-0", Allocatable: cpuMap("4"), Available: cpuMap("99")},
			{ID: "node-1", Allocatable: cpuMap("4"), Available: cpuMap("99")},
		},
	}

	// Bound pod observed on node-1 (cpu 2) → subtracted.
	bound := gPod("bound", map[string]string{"cpu": "2"})
	bound.Status = pod_status.Running
	bound.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-1", "2"))}

	// Pipelined pod (scheduler-committed, incoming) with a record → counted (active-allocated).
	pipelined := gPod("pipelined", map[string]string{"cpu": "2"})
	pipelined.Status = pod_status.Pipelined
	pipelined.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-0", "2"))}

	// Pending pod (not active-allocated) with a record → ignored.
	pending := gPod("pending", map[string]string{"cpu": "1"})
	pending.Status = pod_status.Pending
	pending.Pod.Annotations = map[string]string{commonconstants.NumaPlacementObserved: observedAnnotation(observedZone("node-0", "1"))}

	// Active pod with no record → contributes nothing (never guess a zone).
	noRecord := gPod("norecord", map[string]string{"cpu": "2"})
	noRecord.Status = pod_status.Running

	node := &node_info.NodeInfo{
		Name:         "node-a",
		NumaTopology: topo,
		PodInfos: map[common_info.PodID]*pod_info.PodInfo{
			bound.UID: bound, pipelined.UID: pipelined, pending.UID: pending, noRecord.UID: noRecord,
		},
	}
	pp := &numaPlugin{reconstructAvailable: true}
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{
		Nodes: map[string]*node_info.NodeInfo{"node-a": node},
	}}

	pp.reconstructNodeAvailable(ssn)

	z0 := topo.Zones[0].Available["cpu"]
	z1 := topo.Zones[1].Available["cpu"]
	assert.Equal(t, int64(2), z0.Value(), "node-0: Allocatable 4 minus pipelined 2 (pending excluded, stale 99 discarded)")
	assert.Equal(t, int64(2), z1.Value(), "node-1: Allocatable 4 minus the running pod's observed 2")
}
