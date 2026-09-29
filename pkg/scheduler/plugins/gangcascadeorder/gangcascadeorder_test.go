// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gangcascadeorder

import (
	"strconv"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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

// runningPodOnNode builds a running 1-GPU pod bound to node.
func runningPodOnNode(uid, node string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: uid, Namespace: "ns", UID: types.UID(uid)},
		Spec: v1.PodSpec{
			NodeName: node,
			Containers: []v1.Container{{Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{resource_info.GPUResourceName: resource.MustParse("1")},
			}}},
		},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}
}

// makeGangWithMin builds a running gang across nodes[i] with the given per podset
// minAvailable, so a gang with minAvailable below len(nodes) is elastic with surplus.
func makeGangWithMin(uid string, priority int32, nodes []string, minAvailable int32) *podgroup_info.PodGroupInfo {
	vm := resource_info.NewResourceVectorMap()
	tasks := make([]*pod_info.PodInfo, 0, len(nodes))
	for i, node := range nodes {
		tasks = append(tasks, pod_info.NewTaskInfo(runningPodOnNode(uid+"-"+strconv.Itoa(i), node), vm))
	}
	pg := podgroup_info.NewPodGroupInfoWithVectorMap(common_info.PodGroupID(uid), vm, tasks...)
	for _, ps := range pg.GetAllPodSets() {
		ps.SetMinAvailable(minAvailable)
	}
	pg.Priority = priority
	return pg
}

// A wide gang running above minMember can shed a single surplus pod without cascading, so
// its true cost is one pod on one node, not its full span. The plugin must score it by the
// tasks GetTasksToEvict would actually remove and prefer it over tearing down a smaller
// gang that sits at its minimum.
func TestVictimOrderFn_SurplusAwareElasticIsCheaper(t *testing.T) {
	ssn := &framework.Session{}
	p := &gangCascadeOrderPlugin{ssn: ssn}

	// Elastic: 4 pods across 4 nodes, min 1 -> shedding surplus costs 1 pod on 1 node.
	wideElastic := makeGangWithMin("wide-elastic", 10, []string{"n0", "n1", "n2", "n3"}, 1)
	// At minimum: 3 pods across 3 nodes, min 3 -> evicting cascades the whole gang.
	atMin := makeGangWithMin("at-min", 10, []string{"n4", "n5", "n6"}, 3)

	if span, pods := p.gangFootprint(wideElastic); span != 1 || pods != 1 {
		t.Fatalf("elastic surplus footprint = (span %d, pods %d), want (1, 1)", span, pods)
	}
	if span, pods := p.gangFootprint(atMin); span != 3 || pods != 3 {
		t.Fatalf("at-min footprint = (span %d, pods %d), want (3, 3)", span, pods)
	}
	if got := p.VictimOrderFn(wideElastic, atMin); got != -1 {
		t.Errorf("expected wide elastic gang (sheds 1 surplus pod) evicted before at-min gang: got %d", got)
	}
}
