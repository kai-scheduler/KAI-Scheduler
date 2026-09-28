// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gangcascadeorder

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

// makeGang builds a running gang whose i-th pod lands on nodes[i].
func makeGang(uid string, priority int32, nodes []string, vm *resource_info.ResourceVectorMap) *podgroup_info.PodGroupInfo {
	tasks := make([]*pod_info.PodInfo, 0, len(nodes))
	for i, node := range nodes {
		tasks = append(tasks, &pod_info.PodInfo{
			UID:          common_info.PodID(uid + "-task-" + node + "-" + string(rune('a'+i))),
			ResReqVector: resource_info.NewResourceVectorWithValues(0, 0, 1, vm),
			Status:       pod_status.Running,
			NodeName:     node,
		})
	}
	pg := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(uid), vm, tasks...)
	pg.Priority = priority
	return pg
}

func newPlugin(t *testing.T) *gangCascadeOrderPlugin {
	t.Helper()
	p, ok := New(framework.PluginArguments{}).(*gangCascadeOrderPlugin)
	if !ok {
		t.Fatalf("New() did not return *gangCascadeOrderPlugin")
	}
	return p
}

func TestVictimOrderFn_PriorityDiffers_Defers(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	wide := makeGang("wide", 10, []string{"n0", "n1", "n2", "n3"}, vm)
	narrow := makeGang("narrow", 50, []string{"n4"}, vm)
	p := newPlugin(t)
	// Different priority: the tiebreaker must not engage, even though narrow is far
	// less disruptive — priority ordering owns that decision.
	if got := p.VictimOrderFn(wide, narrow); got != 0 {
		t.Errorf("expected 0 when priorities differ, got %d", got)
	}
}

func TestVictimOrderFn_PrefersSmallerNodeSpan(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	// Same priority, same pod count (4), but different cross-node span.
	narrow := makeGang("narrow", 10, []string{"n0"}, vm)               // span 1
	wide := makeGang("wide", 10, []string{"n0", "n1", "n2", "n3"}, vm) // span 4
	p := newPlugin(t)
	if got := p.VictimOrderFn(narrow, wide); got != -1 {
		t.Errorf("expected narrow (span 1) evicted before wide (span 4): got %d", got)
	}
	if got := p.VictimOrderFn(wide, narrow); got != 1 {
		t.Errorf("expected wide (span 4) evicted after narrow (span 1): got %d", got)
	}
}

// A large self-contained gang must be preferred over a small cross-node gang — the case
// GPU/pod-size tiebreaks get wrong. Freeing the single-node gang cascades nothing.
func TestVictimOrderFn_LargeSingleNodeBeatsSmallCrossNode(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	bigLocal := makeGang("bigLocal", 10, []string{"n0", "n0", "n0", "n0", "n0", "n0", "n0", "n0"}, vm) // span 1, 8 pods
	smallWide := makeGang("smallWide", 10, []string{"n1", "n2"}, vm)                                   // span 2, 2 pods
	p := newPlugin(t)
	if got := p.VictimOrderFn(bigLocal, smallWide); got != -1 {
		t.Errorf("expected large single-node gang evicted before small cross-node gang: got %d", got)
	}
}

func TestVictimOrderFn_EqualSpan_PrefersFewerPods(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	// Both span 1; secondary tiebreak on total pods.
	fewer := makeGang("fewer", 10, []string{"n0"}, vm)           // 1 pod
	more := makeGang("more", 10, []string{"n0", "n0", "n0"}, vm) // 3 pods
	p := newPlugin(t)
	if got := p.VictimOrderFn(fewer, more); got != -1 {
		t.Errorf("expected fewer-pod gang evicted first on equal span: got %d", got)
	}
}

func TestVictimOrderFn_Identical_Zero(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()
	a := makeGang("a", 10, []string{"n0", "n1"}, vm)
	b := makeGang("b", 10, []string{"n2", "n3"}, vm)
	p := newPlugin(t)
	if got := p.VictimOrderFn(a, b); got != 0 {
		t.Errorf("expected 0 for equal span and pod count, got %d", got)
	}
}
