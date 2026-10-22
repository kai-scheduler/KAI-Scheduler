// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package antiaffinity

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8sframework "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	kubernetesframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
)

func TestBindReadyWithReleasingPodAntiAffinity(t *testing.T) {
	const hostname = "kubernetes.io/hostname"
	const zone = "topology.kubernetes.io/zone"
	type setup struct {
		incoming, victim *pod_info.PodInfo
		nodes            map[string]*node_info.NodeInfo
	}
	term := func(selector map[string]string) v1.PodAffinityTerm {
		return v1.PodAffinityTerm{TopologyKey: hostname, LabelSelector: &metav1.LabelSelector{MatchLabels: selector}}
	}
	antiAffinity := func(t v1.PodAffinityTerm) *v1.Affinity {
		return &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{t}}}
	}
	incomingTerm := func(s *setup) *v1.PodAffinityTerm {
		return &s.incoming.Pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0]
	}
	moveVictim := func(s *setup) {
		s.nodes["node1"].PodInfos = s.nodes["node0"].PodInfos
		s.nodes["node0"].PodInfos = nil
	}
	for _, tc := range []struct {
		name   string
		modify func(*setup)
		ready  bool
	}{
		{name: "incoming required anti-affinity", ready: false},
		{name: "virtual releasing victim", modify: func(s *setup) { s.victim.IsVirtualStatus = true }, ready: false},
		{name: "nonmatching pod labels", modify: func(s *setup) { s.victim.Pod.Labels["tier"] = "other" }, ready: true},
		{name: "no anti-affinity", modify: func(s *setup) { s.incoming.Pod.Spec.Affinity = nil }, ready: true},
		{name: "preferred anti-affinity", modify: func(s *setup) {
			a := s.incoming.Pod.Spec.Affinity.PodAntiAffinity
			a.PreferredDuringSchedulingIgnoredDuringExecution = []v1.WeightedPodAffinityTerm{{Weight: 100, PodAffinityTerm: a.RequiredDuringSchedulingIgnoredDuringExecution[0]}}
			a.RequiredDuringSchedulingIgnoredDuringExecution = nil
		}, ready: true},
		{name: "victim required anti-affinity", modify: func(s *setup) {
			s.incoming.Pod.Spec.Affinity = nil
			s.victim.Pod.Spec.Affinity = antiAffinity(term(map[string]string{"tier": "train"}))
		}, ready: false},
		{name: "different hostname", modify: moveVictim, ready: true},
		{name: "same zone on another node", modify: func(s *setup) { moveVictim(s); incomingTerm(s).TopologyKey = zone }, ready: false},
		{name: "different zone", modify: func(s *setup) {
			moveVictim(s)
			incomingTerm(s).TopologyKey = zone
			s.nodes["node1"].Node.Labels[zone] = "zone1"
		}, ready: true},
		{name: "missing topology label", modify: func(s *setup) { delete(s.nodes["node0"].Node.Labels, hostname) }, ready: true},
		{name: "different default namespace", modify: func(s *setup) { s.victim.Pod.Namespace = "victims" }, ready: true},
		{name: "explicit namespace", modify: func(s *setup) { s.victim.Pod.Namespace = "victims"; incomingTerm(s).Namespaces = []string{"victims"} }, ready: false},
		{name: "matching namespace selector", modify: func(s *setup) {
			s.victim.Pod.Namespace = "victims"
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "victims"}}
		}, ready: false},
		{name: "nonmatching namespace selector", modify: func(s *setup) {
			s.victim.Pod.Namespace = "victims"
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "other"}}
		}, ready: true},
		{name: "all namespaces", modify: func(s *setup) {
			s.victim.Pod.Namespace = "victims"
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{}
		}, ready: false},
		{name: "explicit namespace or selector", modify: func(s *setup) {
			s.victim.Pod.Namespace = "victims"
			incomingTerm(s).Namespaces = []string{"victims"}
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "other"}}
		}, ready: false},
		{name: "symmetric namespace selector in another zone node", modify: func(s *setup) {
			moveVictim(s)
			s.incoming.Pod.Spec.Affinity = nil
			t := term(map[string]string{"tier": "train"})
			t.TopologyKey = zone
			t.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "train"}}
			s.victim.Pod.Namespace = "victims"
			s.victim.Pod.Spec.Affinity = antiAffinity(t)
		}, ready: false},
		{name: "namespace selector does not match missing namespace", modify: func(s *setup) {
			s.victim.Pod.Namespace = "missing"
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "victims"}}
		}, ready: true},
		{name: "stuck releasing handled by normal predicates", modify: func(s *setup) { s.victim.Status = pod_status.StuckInReleasing }, ready: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &setup{
				incoming: &pod_info.PodInfo{UID: "incoming", Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "train", Labels: map[string]string{"tier": "train"}}, Spec: v1.PodSpec{Affinity: antiAffinity(term(map[string]string{"tier": "victim"}))}}},
				victim:   &pod_info.PodInfo{UID: "victim", Status: pod_status.Releasing, Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "train", Labels: map[string]string{"tier": "victim"}}}},
				nodes:    map[string]*node_info.NodeInfo{},
			}
			for _, name := range []string{"node0", "node1"} {
				s.nodes[name] = &node_info.NodeInfo{Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{hostname: name, zone: "zone0"}}}}
			}
			s.nodes["node0"].PodInfos = map[common_info.PodID]*pod_info.PodInfo{"victim": s.victim}
			if tc.modify != nil {
				tc.modify(s)
			}
			plugin, upstream := newBindReadyTestPlugin(t, s.nodes)
			if len(k8sframework.GetPodAntiAffinityTerms(s.incoming.Pod.Spec.Affinity)) == 0 && len(k8sframework.GetPodAntiAffinityTerms(s.victim.Pod.Spec.Affinity)) > 0 {
				_, status := upstream.PreFilter(context.Background(), kubernetesframework.NewCycleState(), s.incoming.Pod, nil)
				require.True(t, status.IsSkip(), "ordinary incoming preprocessing must skip this symmetric case")
			}
			ready, err := plugin.IsReadyForBinding(s.incoming, s.nodes["node0"])
			require.NoError(t, err)
			require.Equal(t, tc.ready, ready)
		})
	}
}

func newBindReadyTestPlugin(t *testing.T, nodes map[string]*node_info.NodeInfo) (BindReadiness, *interpodaffinity.InterPodAffinity) {
	t.Helper()
	client := fake.NewClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	for _, name := range []string{"train", "victims"} {
		require.NoError(t, factory.Core().V1().Namespaces().Informer().GetIndexer().Add(&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"team": name}},
		}))
	}
	index := cache.NewK8sClusterPodAffinityInfo()
	for _, node := range nodes {
		info := cluster_info.NewK8sNodePodAffinityInfo(node.Node, index)
		for _, task := range node.PodInfos {
			if task.Status != pod_status.Releasing {
				info.AddPod(task.Pod)
			}
		}
	}
	handle := k8s_utils.NewFrameworkHandle(client, factory, index)
	upstream, err := interpodaffinity.New(context.Background(), &config.InterPodAffinityArgs{}, handle, k8s_utils.GetK8sFeatures())
	require.NoError(t, err)
	for _, node := range nodes {
		for key, task := range node.PodInfos {
			if task.Status == pod_status.Releasing {
				if node.ReleasingPods == nil {
					node.ReleasingPods = make(map[common_info.PodID]*pod_info.PodInfo)
				}
				node.ReleasingPods[key] = task
			}
		}
	}
	return New(nodes, handle, k8s_utils.GetK8sFeatures()), upstream.(*interpodaffinity.InterPodAffinity)
}

func TestBindReadySeparatesReleasingAntiAffinity(t *testing.T) {
	for _, symmetric := range []bool{false, true} {
		t.Run(fmt.Sprintf("symmetric-%t", symmetric), func(t *testing.T) {
			term := func(tier string) v1.PodAffinityTerm {
				return v1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": tier}}}
			}
			pod := func(name string) *pod_info.PodInfo {
				return &pod_info.PodInfo{UID: common_info.PodID(name), Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name), Namespace: "train", Labels: map[string]string{"tier": name}}}}
			}
			incoming, victim, ordinary := pod("incoming"), pod("victim"), pod("ordinary")
			victim.Status = pod_status.Releasing
			ordinary.Status = pod_status.Running
			owner, other := incoming, victim
			if symmetric {
				owner, other = victim, incoming
			}
			owner.Pod.Spec.Affinity = &v1.Affinity{
				PodAffinity:     &v1.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{term("absent")}},
				PodAntiAffinity: &v1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{term(string(other.UID))}},
			}
			ordinary.Pod.Spec.Affinity = &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{term(string(owner.UID))}}}
			nodes := map[string]*node_info.NodeInfo{}
			for _, name := range []string{"node0", "node1"} {
				nodes[name] = &node_info.NodeInfo{Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"kubernetes.io/hostname": name}}}}
			}
			nodes["node0"].PodInfos = map[common_info.PodID]*pod_info.PodInfo{victim.UID: victim, ordinary.UID: ordinary}
			pp, _ := newBindReadyTestPlugin(t, nodes)
			original := owner.Pod.DeepCopy()
			for _, name := range []string{"node0", "node1", "node0"} {
				ready, err := pp.IsReadyForBinding(incoming, nodes[name])
				require.NoError(t, err)
				require.Equal(t, name == "node1", ready)
			}
			require.Equal(t, original, owner.Pod)
		})
	}
}

func TestBindReadySkipsNodesWithoutReleasingPods(t *testing.T) {
	node := &node_info.NodeInfo{PodInfos: map[common_info.PodID]*pod_info.PodInfo{"unreadable": nil}}
	checker := New(map[string]*node_info.NodeInfo{"node0": node}, nil, k8s_utils.GetK8sFeatures())
	ready, err := checker.IsReadyForBinding(nil, nil)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestBindReadyWithoutRequiredAntiAffinity(t *testing.T) {
	node := &node_info.NodeInfo{ReleasingPods: map[common_info.PodID]*pod_info.PodInfo{
		"victim": {UID: "victim", Pod: &v1.Pod{}},
	}}
	checker := New(map[string]*node_info.NodeInfo{"node0": node}, nil, k8s_utils.GetK8sFeatures())
	ready, err := checker.IsReadyForBinding(&pod_info.PodInfo{UID: "incoming", Pod: &v1.Pod{}}, node)
	require.NoError(t, err)
	require.True(t, ready)
}

func BenchmarkBindReadyFastPaths(b *testing.B) {
	for _, nodeCount := range []int{100, 1000, 10000} {
		for _, perNode := range []int{0, 10, 100} {
			b.Run(fmt.Sprintf("%d-nodes-%d-pods-per-node", nodeCount, perNode), func(b *testing.B) {
				nodes := make(map[string]*node_info.NodeInfo, nodeCount)
				for i := 0; i < nodeCount; i++ {
					name := fmt.Sprintf("node%d", i)
					pods := make(map[common_info.PodID]*pod_info.PodInfo, perNode)
					for j := 0; j < perNode; j++ {
						id := common_info.PodID(fmt.Sprintf("pod%d-%d", i, j))
						pods[id] = nil
					}
					nodes[name] = &node_info.NodeInfo{Name: name, PodInfos: pods}
				}
				checker := New(nodes, nil, k8s_utils.GetK8sFeatures())
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ready, err := checker.IsReadyForBinding(nil, nil)
					if err != nil || !ready {
						b.Fatalf("unexpected readiness: %v %v", ready, err)
					}
				}
			})
		}
	}
}

func TestBindReadyReusesImmutablePreparation(t *testing.T) {
	const hostname = "kubernetes.io/hostname"
	incoming := &pod_info.PodInfo{UID: "incoming", Pod: &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "train"},
		Spec: v1.PodSpec{Affinity: &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{{TopologyKey: hostname, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "victim"}}}},
		}}},
	}}
	victim := &pod_info.PodInfo{UID: "victim", Status: pod_status.Releasing, Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "train", Labels: map[string]string{"tier": "victim"}}}}
	nodes := map[string]*node_info.NodeInfo{}
	for _, name := range []string{"node0", "node1"} {
		nodes[name] = &node_info.NodeInfo{Node: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{hostname: name}}}}
	}
	nodes["node0"].PodInfos = map[common_info.PodID]*pod_info.PodInfo{"victim": victim}
	plugin, _ := newBindReadyTestPlugin(t, nodes)
	checker := plugin.(*bindReadiness)
	require.NoError(t, checker.Prepare(incoming, nil))
	original := checker.prepared(incoming)
	require.NotNil(t, original.state)
	var workers sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < cap(results); i++ {
		workers.Go(func() {
			for name, node := range nodes {
				ready, err := checker.IsReadyForBinding(incoming, node)
				if err != nil {
					results <- err
					return
				}
				if ready != (name == "node1") {
					results <- fmt.Errorf("unexpected readiness for %s: %t", name, ready)
					return
				}
			}
			results <- nil
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Same(t, original, checker.prepared(incoming))
	// Equal pod counts must not hide replacement of a conflicting releasing pod.
	nodes["node0"].ReleasingPods["victim"] = &pod_info.PodInfo{UID: "other", Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "train", Labels: map[string]string{"tier": "other"}}}}
	nodes["node0"].ReleasingPodsRevision++
	ready, err := checker.IsReadyForBinding(incoming, nodes["node0"])
	require.NoError(t, err)
	require.True(t, ready)
	require.NotSame(t, original, checker.prepared(incoming))
}
