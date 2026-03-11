// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_internal

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

type NodeFilter interface {
	Filter(ctx context.Context, state ksf.CycleState,
		pod *v1.Pod, nodeInfo ksf.NodeInfo) *ksf.Status
}

type NodePreFilter interface {
	PreFilter(ctx context.Context,
		cycleState ksf.CycleState, pod *v1.Pod, nodes []ksf.NodeInfo) (*k8sframework.PreFilterResult, *ksf.Status)
}

type NodeScorer interface {
	Score(ctx context.Context, cycleState ksf.CycleState, pod *v1.Pod, nodeInfo ksf.NodeInfo) (int64, *ksf.Status)
}

type ExtendedNodeScorer interface {
	NodeScorer
	PreScore(context.Context, ksf.CycleState, *v1.Pod, []ksf.NodeInfo) *ksf.Status
}

type FitPredicateRequired func(pod *v1.Pod) bool
type FitPredicatePreFilter func(pod *v1.Pod) (sets.Set[string], *ksf.Status)
type FitPredicateFilter func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error)

type SessionPredicate struct {
	Name                string
	IsPreFilterRequired FitPredicateRequired
	PreFilter           FitPredicatePreFilter
	IsFilterRequired    FitPredicateRequired
	Filter              FitPredicateFilter
}

type PreScoreFn func(pod *v1.Pod, fittingNodes []ksf.NodeInfo) *ksf.Status
type ScorePredicate func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (int64, []string, error)

type PredicateName string
type SessionPredicates map[PredicateName]SessionPredicate

type SessionScoreFns struct {
	PrePodAffinity PreScoreFn
	PodAffinity    ScorePredicate
}

type SessionStateProvider interface {
	GetSessionStateForResource(podUID types.UID) SessionState
}

type NodeListProvider interface {
	GetNodes() []ksf.NodeInfo
}
