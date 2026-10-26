// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/dynamic-resource-allocation/structured"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
)

// ClusterView is the cluster state a predicate is evaluated against. Predicates read node pods,
// k8s prefilter state and DRA allocations only through the view they are given, so the same
// predicate checks the cluster as it is now or as it will be once its terminating pods are gone.
// KAI resource vectors are not part of the view: they already account for releasing resources.
type ClusterView interface {
	// K8sNodeInfo returns the node as k8s filter plugins see it.
	K8sNodeInfo(node *node_info.NodeInfo) *k8sframework.NodeInfo
	// CycleState returns the pod's k8s prefilter state.
	CycleState(pod *v1.Pod) k8s_internal.SessionState
	// DRAAllocatedState returns the DRA devices that are not available to the pod's claims.
	DRAAllocatedState(pod *v1.Pod) (*structured.AllocatedState, error)
}
