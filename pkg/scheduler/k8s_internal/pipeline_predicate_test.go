// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_internal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8scache "k8s.io/kubernetes/pkg/scheduler/backend/cache"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/parallelize"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/metrics"
)

type pipelineStateProvider struct{ state SessionState }

func (p pipelineStateProvider) GetSessionStateForResource(types.UID) SessionState { return p.state }

type affinityHandle struct {
	ksf.Handle
	lister  ksf.SharedLister
	factory informers.SharedInformerFactory
}

func (h affinityHandle) SnapshotSharedLister() ksf.SharedLister                 { return h.lister }
func (h affinityHandle) SharedInformerFactory() informers.SharedInformerFactory { return h.factory }
func (h affinityHandle) Parallelizer() ksf.Parallelizer                         { return parallelize.NewParallelizer(1) }

func TestPipelinePredicateAffinity(t *testing.T) {
	metrics.Register()
	for _, antiAffinity := range []bool{true, false} {
		name := "required affinity"
		if antiAffinity {
			name = "required anti-affinity"
		}
		t.Run(name, func(t *testing.T) {
			now := metav1.Now()
			victim := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "ns", UID: "victim", Labels: map[string]string{"app": "victim"}, DeletionTimestamp: &now}, Spec: v1.PodSpec{NodeName: "node"}}
			node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{"kubernetes.io/hostname": "node"}}}
			term := v1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "victim"}}}
			affinity := &v1.Affinity{}
			if antiAffinity {
				affinity.PodAntiAffinity = &v1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{term}}
			} else {
				affinity.PodAffinity = &v1.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{term}}
			}
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "incoming", Namespace: "ns", UID: "incoming"}, Spec: v1.PodSpec{Affinity: affinity}}
			snapshot := k8scache.NewSnapshot([]*v1.Pod{victim}, []*v1.Node{node})
			factory := informers.NewSharedInformerFactory(fake.NewClientset(), time.Minute)
			require.NoError(t, factory.Core().V1().Namespaces().Informer().GetIndexer().Add(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}}))
			plugin, err := interpodaffinity.New(context.Background(), &config.InterPodAffinityArgs{}, affinityHandle{lister: snapshot, factory: factory}, feature.Features{})
			require.NoError(t, err)
			ipa := plugin.(*interpodaffinity.InterPodAffinity)
			state := NewSessionState()
			nodes, err := snapshot.NodeInfos().List()
			require.NoError(t, err)
			_, status := ipa.PreFilter(context.Background(), state, pod, nodes)
			require.True(t, status.IsSuccess(), "%v", status)
			info := nodes[0].(*k8sframework.NodeInfo)
			original := info.SnapshotConcrete()
			provider := pipelineStateProvider{state: state}
			allocate := FitPredicateConverter(provider, ipa)
			pipeline := FitPipelinePredicateConverter(provider, ipa)
			fit, _, _ := allocate(pod, info)
			require.Equal(t, !antiAffinity, fit)
			for range 2 {
				fit, _, _ = pipeline(pod, info)
				require.Equal(t, antiAffinity, fit)
				require.Equal(t, original, info)
				fit, _, _ = allocate(pod, info)
				require.Equal(t, !antiAffinity, fit)
			}
		})
	}
}

func TestPipelinePredicateHostPorts(t *testing.T) {
	for _, terminating := range []bool{true, false} {
		t.Run(map[bool]string{true: "terminating", false: "running"}[terminating], func(t *testing.T) {
			ports := []v1.Container{{Name: "container", Ports: []v1.ContainerPort{{HostPort: 8080, Protocol: v1.ProtocolTCP}}}}
			victim := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "victim", UID: "victim"}, Spec: v1.PodSpec{Containers: ports}}
			if terminating {
				now := metav1.Now()
				victim.DeletionTimestamp = &now
			}
			incoming := &v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "incoming"}, Spec: v1.PodSpec{Containers: ports}}
			node := k8sframework.NewNodeInfo(victim)
			node.SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
			original := node.SnapshotConcrete()
			state := NewSessionState()
			plugin := &nodeports.NodePorts{}
			_, status := plugin.PreFilter(context.Background(), state, incoming, nil)
			require.True(t, status.IsSuccess())
			provider := pipelineStateProvider{state: state}
			fit, _, _ := FitPredicateConverter(provider, plugin)(incoming, node)
			require.False(t, fit)
			fit, _, _ = FitPipelinePredicateConverter(provider, plugin)(incoming, node)
			require.Equal(t, terminating, fit)
			require.Equal(t, original, node)
			fit, _, _ = FitPredicateConverter(provider, plugin)(incoming, node)
			require.False(t, fit)
		})
	}
}

type extensionFilter struct {
	filter func(ksf.CycleState, ksf.NodeInfo) *ksf.Status
	add    func(ksf.CycleState, ksf.NodeInfo) *ksf.Status
	remove func(ksf.CycleState, ksf.NodeInfo) *ksf.Status
}

func (f extensionFilter) Filter(_ context.Context, s ksf.CycleState, _ *v1.Pod, n ksf.NodeInfo) *ksf.Status {
	return f.filter(s, n)
}
func (f extensionFilter) PreFilterExtensions() ksf.PreFilterExtensions { return f }
func (f extensionFilter) AddPod(_ context.Context, s ksf.CycleState, _ *v1.Pod, _ ksf.PodInfo, n ksf.NodeInfo) *ksf.Status {
	return f.add(s, n)
}
func (f extensionFilter) RemovePod(_ context.Context, s ksf.CycleState, _ *v1.Pod, _ ksf.PodInfo, n ksf.NodeInfo) *ksf.Status {
	return f.remove(s, n)
}

func TestPipelinePredicateRestoresOnFailure(t *testing.T) {
	for _, failure := range []string{"remove", "filter", "add"} {
		t.Run(failure, func(t *testing.T) {
			now := metav1.Now()
			node := k8sframework.NewNodeInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "victim", DeletionTimestamp: &now}})
			node.SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
			original := node.SnapshotConcrete()
			state := NewSessionState()
			adds := 0
			plugin := extensionFilter{
				remove: func(s ksf.CycleState, n ksf.NodeInfo) *ksf.Status {
					require.Empty(t, n.GetPods())
					s.Write("changed", &testState{})
					if failure == "remove" {
						return ksf.NewStatus(ksf.Error, "remove failed")
					}
					return nil
				},
				filter: func(_ ksf.CycleState, n ksf.NodeInfo) *ksf.Status {
					require.NotEqual(t, "remove", failure)
					require.Empty(t, n.GetPods())
					if failure == "filter" {
						return ksf.NewStatus(ksf.Error, "filter failed")
					}
					return nil
				},
				add: func(_ ksf.CycleState, n ksf.NodeInfo) *ksf.Status {
					adds++
					require.Len(t, n.GetPods(), 1)
					if failure == "add" {
						return ksf.NewStatus(ksf.Error, "add failed")
					}
					return nil
				},
			}
			fit, _, err := FitPipelinePredicateConverter(pipelineStateProvider{state: state}, plugin)(&v1.Pod{}, node)
			require.False(t, fit)
			require.ErrorContains(t, err, failure+" failed")
			require.Equal(t, 1, adds)
			require.Equal(t, original, node)
			_, err = state.Read("changed")
			require.Error(t, err)
		})
	}
}

type testState struct{}

func (*testState) Clone() ksf.StateData { return &testState{} }
