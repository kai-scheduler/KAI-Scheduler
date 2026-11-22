// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"fmt"
	"testing"

	nrt "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestUnchangedNodeResourceTopologyStateID(t *testing.T) {
	cluster := &api.ClusterInfo{Nodes: map[string]*node_info.NodeInfo{}}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("node-%d", i)
		cluster.Nodes[name] = &node_info.NodeInfo{
			Node: cpuNode(name, "4"),
			NodeResourceTopology: &nrt.NodeResourceTopology{
				ObjectMeta: metav1.ObjectMeta{Name: name},
			},
		}
	}
	args := framework.PluginArguments{"clusterName": t.Name()}
	var firstID string
	var firstOrder []string
	for i := 0; i < 128; i++ {
		p := newPlugin(args, nil)
		p.reuse = streamReuse(args)
		cycle := &cycleRecord{}
		cycle.RawObjects = objectsFromCluster(cluster, &conf.SchedulerParams{})
		var order []string
		for _, obj := range cycle.RawObjects.NodeResourceTopologies {
			order = append(order, obj.Name)
		}
		embedded := p.retainState(cycle)
		if i == 0 {
			if !embedded {
				t.Fatal("first record expected to include body")
			}
			p.noteSavedState(cycle, true)
			firstID, firstOrder = cycle.StateID, order
			continue
		}
		if embedded || cycle.RawObjects != nil || cycle.StateID != firstID {
			t.Fatalf("first capture: id=%s order=%v; capture %d: id=%s body=%t order=%v",
				firstID, firstOrder, i+1, cycle.StateID, cycle.RawObjects != nil, order)
		}
	}
}
