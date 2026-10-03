// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
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
		{name: "namespace lookup failure", modify: func(s *setup) {
			s.victim.Pod.Namespace = "missing"
			incomingTerm(s).NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "victims"}}
		}, ready: false},
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
			factory := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
			for _, name := range []string{"train", "victims"} {
				require.NoError(t, factory.Core().V1().Namespaces().Informer().GetIndexer().Add(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"team": name}}}))
			}
			cacheMock := cache.NewMockCache(gomock.NewController(t))
			cacheMock.EXPECT().KubeInformerFactory().Return(factory).AnyTimes()
			ssn := &framework.Session{Cache: cacheMock, ClusterInfo: &api.ClusterInfo{Nodes: s.nodes}}
			plugin := &predicatesPlugin{ssn: ssn}
			plugin.initializeReleasingTasks()
			ready, err := plugin.bindReady(s.incoming, s.nodes["node0"])
			if tc.name == "namespace lookup failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.ready, ready)
		})
	}
}

func TestBindReadyFastPaths(t *testing.T) {
	for _, releasing := range []bool{false, true} {
		t.Run(fmt.Sprintf("releasing-%t", releasing), func(t *testing.T) {
			victim := &pod_info.PodInfo{UID: "victim", Status: pod_status.Running, Pod: &v1.Pod{}}
			if releasing {
				victim.Status = pod_status.Releasing
			}
			node := &node_info.NodeInfo{PodInfos: map[common_info.PodID]*pod_info.PodInfo{"victim": victim}}
			pp := &predicatesPlugin{ssn: &framework.Session{ClusterInfo: &api.ClusterInfo{Nodes: map[string]*node_info.NodeInfo{"node0": node}}}}
			pp.initializeReleasingTasks()
			// Fast paths must not inspect cached pods or the cluster after initialization.
			victim.Pod = nil
			pp.ssn.ClusterInfo.Nodes = nil
			ready, err := pp.bindReady(&pod_info.PodInfo{Pod: &v1.Pod{}}, nil)
			require.NoError(t, err)
			require.True(t, ready)
			pp.OnSessionClose(pp.ssn)
			require.Nil(t, pp.releasingTasks)
			require.Nil(t, pp.releasingTasksWithAntiAffinity)
		})
	}
}

func BenchmarkBindReadyFastPaths(b *testing.B) {
	for _, status := range []pod_status.PodStatus{pod_status.Running, pod_status.Releasing} {
		for _, perNode := range []int{0, 10, 100} {
			b.Run(fmt.Sprintf("%s/1000-nodes-%d-pods-per-node", status, perNode), func(b *testing.B) {
				nodes := make(map[string]*node_info.NodeInfo, 1000)
				for i := 0; i < 1000; i++ {
					name := fmt.Sprintf("node%d", i)
					pods := make(map[common_info.PodID]*pod_info.PodInfo, perNode)
					for j := 0; j < perNode; j++ {
						id := common_info.PodID(fmt.Sprintf("pod%d-%d", i, j))
						pods[id] = &pod_info.PodInfo{UID: id, Status: status, Pod: &v1.Pod{}}
					}
					nodes[name] = &node_info.NodeInfo{Name: name, PodInfos: pods}
				}
				pp := &predicatesPlugin{ssn: &framework.Session{ClusterInfo: &api.ClusterInfo{Nodes: nodes}}}
				pp.initializeReleasingTasks()
				task := &pod_info.PodInfo{UID: "incoming", Pod: &v1.Pod{}}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ready, err := pp.bindReady(task, nodes["node0"])
					if err != nil || !ready {
						b.Fatalf("unexpected readiness: %v %v", ready, err)
					}
				}
			})
		}
	}
}
