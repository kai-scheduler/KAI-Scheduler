// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_internal

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/kubelet/lifecycle"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

func NewInsufficientResourceError(
	resourceName v1.ResourceName, requested, used, capacity string, distributedTaskMessage bool,
) string {
	if distributedTaskMessage {
		return fmt.Sprintf("Node didn't have enough resources: %s, taking all considerations into account", resourceName)
	}
	return fmt.Sprintf("Node didn't have enough resources: %s, requested: %s, used: %s, capacity: %s",
		resourceName, requested, used, capacity)
}

func NewInsufficientResourceErrorScalarResources(
	resourceName v1.ResourceName, requested, used, capacity int64, distributedTaskMessage bool,
) string {
	if distributedTaskMessage {
		return fmt.Sprintf("Node didn't have enough resources: %s, taking all considerations into account", resourceName)
	}
	return (&lifecycle.InsufficientResourceError{
		ResourceName: resourceName,
		Requested:    requested,
		Used:         used,
		Capacity:     capacity}).Error()
}

func NewInsufficientGpuMemoryCapacity(requested, capacity int64, distributedTaskMessage bool) string {
	if distributedTaskMessage {
		return "Node didn't have enough resources: GPU memory, taking all considerations into account"
	}
	return fmt.Sprintf(
		"Node didn't have enough resources: Each gpu on the node has a gpu memory capacity of %d Mib."+
			" %d Mib of gpu memory has been requested.", capacity, requested)
}

/*************** Converters ***************/

func FitPrePredicateConverter(
	nodeListProvider NodeListProvider,
	stateProvider SessionStateProvider,
	nodePreFilter NodePreFilter,
) FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		state := stateProvider.GetSessionStateForResource(pod.UID)
		result, status := nodePreFilter.PreFilter(context.Background(), state, pod, nodeListProvider.GetNodes())
		if status != nil {
			return nil, status
		}
		if result != nil {
			return result.NodeNames, nil
		}
		return nil, nil
	}
}

func FitPredicateConverter(
	stateProvider SessionStateProvider,
	nodeFilter NodeFilter,
) FitPredicateFilter {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		state := stateProvider.GetSessionStateForResource(pod.UID)
		result := nodeFilter.Filter(context.Background(), state, pod, nodeInfo)
		if result == nil {
			return true, nil, nil
		}
		return result.IsSuccess(), result.Reasons(), result.AsError()
	}
}

func PreScorePluginConverter(
	stateProvider SessionStateProvider,
	nodeScorer ExtendedNodeScorer,
) PreScoreFn {
	return func(pod *v1.Pod, fittingNodes []ksf.NodeInfo) *ksf.Status {
		state := stateProvider.GetSessionStateForResource(pod.UID)
		status := nodeScorer.PreScore(context.Background(), state, pod, fittingNodes)
		return status
	}
}

func ScorePluginConverter(
	stateProvider SessionStateProvider,
	nodeScorer NodeScorer,
) ScorePredicate {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (int64, []string, error) {
		state := stateProvider.GetSessionStateForResource(pod.UID)

		score, result := nodeScorer.Score(context.Background(), state, pod, nodeInfo)
		if result == nil {
			return score, nil, nil
		}
		return 0, result.Reasons(), result.AsError()
	}
}

type SessionState ksf.CycleState

func NewSessionState() SessionState {
	return k8sframework.NewCycleState()
}
